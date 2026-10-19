// This process fixture exercises the real updater/launcher boundary without
// opening any user configuration or project data.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"denova/config"
	"denova/internal/buildinfo"
	"denova/internal/update"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println(buildinfo.Version)
		return
	}
	recovering, err := update.PrepareStartup()
	if err != nil {
		panic(err)
	}
	if recovering {
		return
	}
	// Use the application's real defaults without loading user configuration.
	port := flag.Int("port", *config.DefaultSettings().BackendPort, "test server port")
	_ = flag.Bool("no-open", false, "no browser")
	flag.Parse()
	listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(*port))
	if err != nil {
		panic(err)
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port
	root := os.Getenv("DENOVA_UPDATE_TEST_ROOT")
	if root == "" {
		panic("test root required")
	}
	_ = os.WriteFile(filepath.Join(root, "pid"), []byte(strconv.Itoa(os.Getpid())), 0o600)
	_ = os.WriteFile(filepath.Join(root, "port"), []byte(strconv.Itoa(actualPort)), 0o600)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/update/status", func(w http.ResponseWriter, r *http.Request) {
		status, err := update.NewService().Status()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_ = json.NewEncoder(w).Encode(status)
	})
	mux.HandleFunc("/apply", func(w http.ResponseWriter, r *http.Request) {
		service := update.NewService()
		status, err := service.Status()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		result, err := service.Apply(r.Context(), func() {
			_ = os.WriteFile(filepath.Join(root, "closed"), []byte("closed before exit"), 0o600)
		}, actualPort, status.ID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	})
	go func() {
		defer func() {
			if err := recover(); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		}()
		if err := update.ConfirmReady(context.Background(), "http://127.0.0.1:"+strconv.Itoa(actualPort), buildinfo.Version); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}()
	if err := http.Serve(listener, mux); err != nil {
		panic(err)
	}
}
