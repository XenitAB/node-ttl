package ttl

import (
	"context"
	"time"

	"k8s.io/client-go/kubernetes"
)

func Run(ctx context.Context, client kubernetes.Interface, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			err := evictNextExpiredNode(ctx, client)
			if err != nil {
				return err
			}
		}
	}
}
