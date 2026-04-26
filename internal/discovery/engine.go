package discovery

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"infra-diagram/internal/crawler"
)

type Engine struct {
	crawlers []crawler.Crawler
	driver   neo4j.DriverWithContext
}

func NewEngine(driver neo4j.DriverWithContext, crawlers []crawler.Crawler) *Engine {
	return &Engine{driver: driver, crawlers: crawlers}
}

func (e *Engine) Run(ctx context.Context) error {
	// --- Fase 1: Ingesta de nodos en paralelo ---
	log.Println("Phase 1: ingesting nodes...")
	if err := e.runPhase(ctx, "nodes", func(c crawler.Crawler) error {
		return c.IngestNodes(ctx, e.driver)
	}); err != nil {
		return fmt.Errorf("phase 1 failed: %w", err)
	}

	// --- Fase 2: Mapeo de relaciones en paralelo ---
	log.Println("Phase 2: ingesting relations...")
	if err := e.runPhase(ctx, "relations", func(c crawler.Crawler) error {
		return c.IngestRelations(ctx, e.driver)
	}); err != nil {
		return fmt.Errorf("phase 2 failed: %w", err)
	}

	log.Println("Discovery completed")
	return nil
}

// runPhase ejecuta una fase del discovery corriendo `action` sobre cada
// crawler concurrentemente. Loggea errores individuales pero solo retorna
// error si TODOS los crawlers fallaron — best effort por defecto.
func (e *Engine) runPhase(ctx context.Context, name string, action func(crawler.Crawler) error) error {
	var wg sync.WaitGroup
	errs := make(chan error, len(e.crawlers))

	for _, c := range e.crawlers {
		wg.Add(1)
		go func(cr crawler.Crawler) {
			defer wg.Done()
			if err := action(cr); err != nil {
				errs <- err
			}
		}(c)
	}

	wg.Wait()
	close(errs)

	failures := 0
	var lastErr error
	for err := range errs {
		log.Printf("[%s] crawler error: %v", name, err)
		failures++
		lastErr = err
	}

	if failures == len(e.crawlers) {
		return fmt.Errorf("all crawlers failed in phase %q, last error: %w", name, lastErr)
	}
	return nil
}
