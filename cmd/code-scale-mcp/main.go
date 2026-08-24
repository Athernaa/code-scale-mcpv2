package main

import (
	"context"
	"crypto/subtle"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/Athernaa/code-scale-mcpv2/internal/server"
	"github.com/Athernaa/code-scale-mcpv2/internal/storage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var version = "dev"

func main() {
	transport := flag.String("transport", "stdio", "Transport type: stdio or sse")
	port := flag.Int("port", 8080, "Port for SSE transport")
	showVersion := flag.Bool("version", false, "Show version")
	flag.Parse()

	if *showVersion {
		fmt.Printf("code-scale-mcp %s\n", version)
		os.Exit(0)
	}

	server.Version = version

	store, err := storage.NewIndexStore("")
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Cleanup stale repos and orphaned content directories
	removedRepos, removedDirs, cleanupErr := store.CleanupStale()
	if cleanupErr != nil {
		log.Printf("Warning: stale cleanup error: %v", cleanupErr)
	}
	if len(removedRepos) > 0 {
		log.Printf("Cleaned up %d stale repos: %s", len(removedRepos), strings.Join(removedRepos, ", "))
	}
	if len(removedDirs) > 0 {
		log.Printf("Cleaned up %d orphaned dirs: %s", len(removedDirs), strings.Join(removedDirs, ", "))
	}

	srv, watchMgr := server.NewCodeScaleServer(store)
	defer watchMgr.Close()

	// Restore persisted watches from previous sessions
	if err := watchMgr.RestoreWatches(); err != nil {
		log.Printf("Warning: failed to restore watches: %v", err)
	}

	switch *transport {
	case "stdio":
		ctx := context.Background()
		if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	case "sse":
		addr := fmt.Sprintf(":%d", *port)
		log.Printf("Starting SSE server on %s", addr)
		sseHandler := mcp.NewSSEHandler(func(request *http.Request) *mcp.Server {
			return srv
		}, nil)

		var handler http.Handler = sseHandler
		if token := os.Getenv("CODE_SCALE_AUTH_TOKEN"); token != "" {
			handler = authMiddleware(sseHandler, token)
			log.Printf("SSE server authentication enabled")
		} else {
			log.Printf("WARNING: No CODE_SCALE_AUTH_TOKEN set, SSE server is unauthenticated")
		}
		log.Fatal(http.ListenAndServe(addr, handler))
	default:
		log.Fatalf("Unknown transport: %s (use stdio or sse)", *transport)
	}
}

// authMiddleware wraps an HTTP handler with Bearer token authentication.
func authMiddleware(next http.Handler, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		provided := strings.TrimPrefix(auth, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
