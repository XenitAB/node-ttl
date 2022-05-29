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
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/xenitab/node-ttl/internal/ttl"
)

func main() {
	zapLog, err := zap.NewProduction()
	if err != nil {
		panic(fmt.Sprintf("who watches the watchmen (%v)?", err))
	}
	log := zapr.NewLogger(zapLog)
	if err := run(log); err != nil {
		log.Error(err, "runtime error")
		os.Exit(1)
	}
}

func run(log logr.Logger) error {
	cfg, err := loadConfig(os.Args[1:])
	if err != nil {
		return fmt.Errorf("could not load config: %w", err)
	}
	client, err := getKubernetesClients(cfg.KubeConfigPath)
	if err != nil {
		return fmt.Errorf("could not create Kubernetes client: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	g, ctx := errgroup.WithContext(ctx)
	ctx = logr.NewContext(ctx, log)

	g.Go(func() error {
		err := ttl.Run(ctx, client, cfg.Interval)
		if err != nil {
			return err
		}
		return nil
	})

	handler := http.NewServeMux()
	handler.HandleFunc("/readyz", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := http.Server{Addr: ":8080", Handler: handler}
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
	<-ctx.Done()
	log.Info("shutting down")
	if err := g.Wait(); err != nil {
		return fmt.Errorf("shutdown error: %w", err)
	}
	log.Info("gracefully shutdown")
	return nil
}

type config struct {
	KubeConfigPath string        `arg:"--kubeconfig" help:"path to the kubeconfig file"`
	Interval       time.Duration `arg:"--interval" default:"10m" help:"interval at which to evaluate node ttl"`
}

func loadConfig(args []string) (config, error) {
	argCfg := arg.Config{
		Program:   "node-ttl",
		IgnoreEnv: true,
	}
	var cfg config
	parser, err := arg.NewParser(argCfg, &cfg)
	if err != nil {
		return config{}, err
	}
	err = parser.Parse(args)
	if err != nil {
		return config{}, err
	}
	return cfg, nil
}

func getKubernetesClients(path string) (kubernetes.Interface, error) {
	cfg, err := getKubernetesConfig(path)
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func getKubernetesConfig(path string) (*rest.Config, error) {
	if path != "" {
		cfg, err := clientcmd.BuildConfigFromFlags("", path)
		if err != nil {
			return nil, err
		}
		return cfg, nil
	}
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	return cfg, nil
}
