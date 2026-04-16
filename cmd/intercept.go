package cmd

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AgusRdz/probe/config"
	"github.com/AgusRdz/probe/observer"
	"github.com/AgusRdz/probe/proxy"
	"github.com/AgusRdz/probe/store"
)

// RunIntercept runs `probe intercept --target <url> [flags]`.
func RunIntercept(args []string, cfg *config.Config) {
	fs := flag.NewFlagSet("intercept", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: probe intercept --target <url> [flags]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "flags:")
		fs.PrintDefaults()
	}

	target := fs.String("target", "", "upstream URL to proxy (required, http:// or https://)")
	port := fs.Int("port", cfg.Proxy.Port, "local port to listen on")
	bind := fs.String("bind", cfg.Proxy.Bind, "local bind address")
	filter := fs.String("filter", "", "only capture paths with this prefix")
	ignore := fs.String("ignore", "", "comma-separated path prefixes to skip")
	db := fs.String("db", "", "override DB path")
	grpcReflect := fs.Bool("grpc-reflect", false, "enable gRPC server reflection (Phase 5)")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if *target == "" {
		fmt.Fprintln(os.Stderr, "probe intercept: --target is required")
		fs.Usage()
		os.Exit(1)
	}

	dbPath := *db
	if dbPath == "" {
		var err error
		dbPath, err = store.DBPathForTarget(*target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "probe: resolve db path: %v\n", err)
			os.Exit(1)
		}
	}

	s, err := store.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe: open store: %v\n", err)
		os.Exit(1)
	}

	p, err := proxy.New(*target, s, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe: %v\n", err)
		s.Close() //nolint:errcheck
		os.Exit(1)
	}

	addr := fmt.Sprintf("%s:%d", *bind, *port)
	fmt.Printf("probe: intercepting → %s  listening on %s  db: %s\n", *target, addr, dbPath)

	srv := &http.Server{
		Addr:    addr,
		Handler: p.Handler(*filter, *ignore),
	}

	// Graceful shutdown on SIGINT / SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		fmt.Fprintln(os.Stderr, "\nprobe: shutting down…")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "probe: http shutdown: %v\n", err)
		}
		if err := p.Shutdown(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "probe: proxy shutdown: %v\n", err)
		}
		if err := s.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "probe: store close: %v\n", err)
		}
	}()

	if *grpcReflect {
		go runGRPCReflect(*target, s)
	}

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "probe: listen: %v\n", err)
		os.Exit(1)
	}
}

// runGRPCReflect calls the gRPC reflection API on the target and stores discovered
// services as endpoints. Runs in a goroutine after the proxy starts listening.
func runGRPCReflect(targetURL string, s *store.Store) {
	services, err := observer.ReflectGRPCFromTarget(targetURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe: gRPC reflect: %v\n", err)
		return
	}

	for _, svc := range services {
		fmt.Printf("gRPC: %s (%d methods)\n", svc.ServiceName, len(svc.Methods))
		for _, m := range svc.Methods {
			path := "/" + svc.ServiceName + "/" + m.Name
			_, err := s.UpsertScannedEndpoint(store.ScannedEndpointInput{
				Method:      "POST",
				PathPattern: path,
				Protocol:    "grpc",
				Framework:   "grpc",
				ReqSchema:   m.ReqSchema,
				RespSchema:  m.RespSchema,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "probe: gRPC reflect store %s: %v\n", path, err)
			}
		}
	}
}
