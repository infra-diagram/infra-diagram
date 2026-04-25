package discovery

import (
	"context"
	"fmt"
	"infra-diagram/internal/crawler"
	"sync"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type Engine struct {
	crawlers []crawler.Crawler
	driver   neo4j.DriverWithContext
}

func NewEngine(driver neo4j.DriverWithContext, crawlers []crawler.Crawler) *Engine {
	return &Engine{driver: driver, crawlers: crawlers}
}

func (e *Engine) Run(ctx context.Context) error {
	// Fase 1 - Ingesta de todos los nodos en paralelo
	fmt.Printf("Ingesting nodes...")
	var wg sync.WaitGroup

	errs := make(chan error, len(e.crawlers))

	for _, c := range e.crawlers {
		wg.Add(1)
		go func(cr crawler.Crawler) {
			defer wg.Done()
			if err := cr.IngestNodes(ctx, e.driver); err != nil {
				errs <- err
			}
		}(c)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		return fmt.Errorf("node ingestion error: %w", err)
	}

	// Fase 2 - Todas las relaciones en paralelo
	fmt.Printf("Ingesting relations...")
	errs = make(chan error, len(e.crawlers))

	for _, c := range e.crawlers {
		wg.Add(1)
		go func(cr crawler.Crawler) {
			defer wg.Done()
			if err := cr.IngestRelations(ctx, e.driver); err != nil {
				errs <- err
			}
		}(c)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		return fmt.Errorf("relation ingestion error: %w", err)
	}

	return nil
}
