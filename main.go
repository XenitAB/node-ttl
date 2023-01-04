package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/alexflint/go-arg"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/xenitab/pkg/kubernetes"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/types"

	"github.com/xenitab/node-ttl/internal/ttl"
)

//nolint:lll //ignore
type config struct {
	KubeConfigPath           string        `arg:"--kubeconfig" help:"path to the kubeconfig file"`
	Interval                 time.Duration `arg:"--interval" default:"10m" help:"interval at which to evaluate node ttl"`
	NodePoolMinCheck         bool          `arg:"--min-check" default:"true" help:"check if node pool min size will not allow scale down"`
	StatusConfigMapName      string        `arg:"--status-config-map-name" default:"cluster-autoscaler-status" help:"Cluster autoscaler status configmap name"`
	StatusConfigMapNamespace string        `arg:"--status-config-map-namespace" default:"cluster-autoscaler" help:"Cluster autoscaler status configmap namespace"`
}

func main() {
	var cfg config
	arg.MustParse(&cfg)

	zapLog, err := zap.NewProduction()
	if err != nil {
		fmt.Printf("who watches the watchmen (%v)?\n", err)
		os.Exit(1)
	}
	log := zapr.NewLogger(zapLog)

	if err := run(log, cfg); err != nil {
		log.Error(err, "runtime error")
		os.Exit(1)
	}
}

func run(log logr.Logger, cfg config) error {
	clientset, err := kubernetes.GetKubernetesClientset(cfg.KubeConfigPath)
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	g, ctx := errgroup.WithContext(ctx)
	ctx = logr.NewContext(ctx, log)

	g.Go(func() error {
		var nn *types.NamespacedName
		if cfg.NodePoolMinCheck {
			nn = &types.NamespacedName{Namespace: cfg.StatusConfigMapNamespace, Name: cfg.StatusConfigMapName}
		}
		err := ttl.Run(ctx, clientset, cfg.Interval, nn)
		if err != nil {
			return err
		}
		return nil
	})

	handler := http.NewServeMux()
	handler.HandleFunc("/readyz", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := http.Server{Addr: ":8080", ReadHeaderTimeout: 10 * time.Second, Handler: handler}
	g.Go(func() error {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})
	g.Go(func() error {
		<-ctx.Done()
		if err := srv.Shutdown(context.Background()); err != nil {
			return err
		}
		return nil
	})

	log.Info("running")
	if err := g.Wait(); err != nil {
		return fmt.Errorf("shutdown error: %w", err)
	}
	log.Info("gracefully shutdown")
	return nil
}
