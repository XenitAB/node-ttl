package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
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

	cfg, err := loadConfig(os.Args[1:])
	if err != nil {
		log.Error(err, "could not load config")
		os.Exit(1)
	}
	client, err := getKubernetesClients(cfg.KubeConfigPath)
	if err != nil {
		log.Error(err, "could not create Kubernetes client", "path", cfg.KubeConfigPath)
		os.Exit(1)
	}

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(stopCh)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g, ctx := errgroup.WithContext(ctx)
	ctx = logr.NewContext(ctx, log)

	g.Go(func() error {
		err := ttl.Run(ctx, client, cfg.Interval)
		if err != nil {
			fmt.Println(err)
			return err
		}
		return nil
	})

	handler := http.NewServeMux()
	handler.HandleFunc("/readyz", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	server := http.Server{Addr: ":8080", Handler: handler}
	g.Go(server.ListenAndServe)

	log.Info("running")
	select {
	case <-stopCh:
		break
	case <-ctx.Done():
		break
	}
	cancel()
	log.Info("shutting down")

	timeoutCtx, timeoutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer timeoutCancel()
	if err := server.Shutdown(timeoutCtx); err != nil {
		log.Error(err, "error when shutting down HTTP server")
	}

	if err := g.Wait(); err != nil {
		log.Error(err, "shutdown error")
	}
	log.Info("gracefully shutdown")
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
