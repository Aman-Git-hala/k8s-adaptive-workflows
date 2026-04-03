package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/aman-githala/k8s-adaptive-workflows/api/v1"
)

func uiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ui",
		Short: "Start the local web dashboard",
		RunE: func(cmd *cobra.Command, args []string) error {
			port := "8080"

			// API Handlers
			http.HandleFunc("/api/workflows", handleWorkflows)
			http.HandleFunc("/api/workflows/", handleWorkflowDetail)

			// Serve static front-end files
			fs := http.FileServer(http.Dir("ui/dist"))
			http.Handle("/", fs)

			fmt.Printf("Starting web dashboard at http://localhost:%s\n", port)

			// CORS wrapper (since Vite dev server runs on another port during dev)
			handler := corsMiddleware(http.DefaultServeMux)

			if err := http.ListenAndServe(":"+port, handler); err != nil {
				return err
			}
			return nil
		},
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers",
			"Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func handleWorkflows(w http.ResponseWriter, r *http.Request) {
	c, err := getClient()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if r.Method == http.MethodGet {
		wfList := &v1.AdaptiveWorkflowList{}
		opts := []client.ListOption{}

		if namespace != "" && namespace != "all" {
			opts = append(opts, client.InNamespace(namespace))
		}

		if err := c.List(ctx, wfList, opts...); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(wfList); err != nil {
			log.Printf("failed to encode workflow list: %v", err)
		}
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Yaml string `json:"yaml"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		obj := &unstructured.Unstructured{}
		dec := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
		_, _, err = dec.Decode([]byte(req.Yaml), nil, obj)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to parse yaml metadata: %v", err), http.StatusBadRequest)
			return
		}

		wf := &v1.AdaptiveWorkflow{}
		_, _, err = dec.Decode([]byte(req.Yaml), nil, wf)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to parse as AdaptiveWorkflow: %v", err), http.StatusBadRequest)
			return
		}

		if wf.Namespace == "" {
			wf.Namespace = namespace
		}

		existing := &v1.AdaptiveWorkflow{}
		err = c.Get(ctx, types.NamespacedName{Name: wf.Name, Namespace: wf.Namespace}, existing)
		if err != nil {
			if err := c.Create(ctx, wf); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusCreated)
			if err := json.NewEncoder(w).Encode(map[string]string{"status": "created", "name": wf.Name}); err != nil {
				log.Printf("failed to encode create response: %v", err)
			}
		} else {
			existing.Spec = wf.Spec
			if err := c.Update(ctx, existing); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
			if err := json.NewEncoder(w).Encode(map[string]string{"status": "updated", "name": wf.Name}); err != nil {
				log.Printf("failed to encode update response: %v", err)
			}
		}
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func handleWorkflowDetail(w http.ResponseWriter, r *http.Request) {
	c, err := getClient()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	name := parts[len(parts)-1]

	if name == "" {
		http.Error(w, "Missing name parameter", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if r.Method == http.MethodGet {
		wf := &v1.AdaptiveWorkflow{}
		if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, wf); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(wf); err != nil {
			log.Printf("failed to encode workflow: %v", err)
		}
		return
	}

	if r.Method == http.MethodDelete {
		wf := &v1.AdaptiveWorkflow{}
		wf.Name = name
		wf.Namespace = namespace
		if err := c.Delete(ctx, wf); err != nil {
			log.Printf("Delete error: %s", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "deleted"}); err != nil {
			log.Printf("failed to encode delete response: %v", err)
		}
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}
