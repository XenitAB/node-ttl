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
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/xenitab/pkg/kubernetes"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/types"

	"github.com/xenitab/node-ttl/internal/ttl"
)

//nolint:lll //ignore
type arguments struct {
	ProbeAddr                string        `arg:"--probe-addr" default:":8080" help:"address to serve probe."`
	MetricsAddr              string        `arg:"--metrics-addr" default:":9090" help:"address to serve metrics."`
	KubeConfigPath           string        `arg:"--kubeconfig" help:"path to the kubeconfig file"`
	Interval                 time.Duration `arg:"--interval" default:"10m" help:"interval at which to evaluate node ttl"`
	NodePoolMinCheck         bool          `arg:"--min-check" default:"true" help:"check if node pool min size will not allow scale down"`
	StatusConfigMapName      string        `arg:"--status-config-map-name" default:"cluster-autoscaler-status" help:"Cluster autoscaler status configmap name"`
	StatusConfigMapNamespace string        `arg:"--status-config-map-namespace" default:"cluster-autoscaler" help:"Cluster autoscaler status configmap namespace"`
}

func main() {
	args := &arguments{}
	arg.MustParse(args)

	zapLog, err := zap.NewProduction()
	if err != nil {
		fmt.Printf("who watches the watchmen (%v)?\n", err)
		os.Exit(1)
	}
	log := zapr.NewLogger(zapLog)

	if err := run(log, args); err != nil {
		log.Error(err, "runtime error")
		os.Exit(1)
	}
	log.Info("gracefully shutdown")
}

func run(log logr.Logger, args *arguments) error {
	clientset, err := kubernetes.GetKubernetesClientset(args.KubeConfigPath)
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	g, ctx := errgroup.WithContext(ctx)
	ctx = logr.NewContext(ctx, log)

	g.Go(func() error {
		var nn *types.NamespacedName
		if args.NodePoolMinCheck {
			nn = &types.NamespacedName{Namespace: args.StatusConfigMapNamespace, Name: args.StatusConfigMapName}
		}
		err := ttl.Run(ctx, clientset, args.Interval, nn)
		if err != nil {
			return err
		}
		return nil
	})

	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())
	metricsSrv := &http.Server{
		Addr:    args.MetricsAddr,
		Handler: metricsMux,
	}
	g.Go(func() error {
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})
	g.Go(func() error {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return metricsSrv.Shutdown(shutdownCtx)
	})

	probeMux := http.NewServeMux()
	probeMux.HandleFunc("/readyz", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	probeSrv := http.Server{
		Addr:              args.ProbeAddr,
		ReadHeaderTimeout: 10 * time.Second,
		Handler:           probeMux,
	}
	g.Go(func() error {
		if err := probeSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})
	g.Go(func() error {
		<-ctx.Done()
		if err := probeSrv.Shutdown(context.Background()); err != nil {
			return err
		}
		return nil
	})

	log.Info("running Node TTL")
	if err := g.Wait(); err != nil {
		return err
	}
	return nil
}
