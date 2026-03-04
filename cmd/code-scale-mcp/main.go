package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/syphon1c/code-scale-mcp/internal/server"
	"github.com/syphon1c/code-scale-mcp/internal/storage"
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

	srv, watchMgr := server.NewCodeScaleServer(store)
	defer watchMgr.Close()

	switch *transport {
	case "stdio":
		ctx := context.Background()
		if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	case "sse":
		addr := fmt.Sprintf(":%d", *port)
		log.Printf("Starting SSE server on %s", addr)
		handler := mcp.NewSSEHandler(func(request *http.Request) *mcp.Server {
			return srv
		}, nil)
		log.Fatal(http.ListenAndServe(addr, handler))
	default:
		log.Fatalf("Unknown transport: %s (use stdio or sse)", *transport)
	}
}
