/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	stdruntime "runtime"
	"runtime/debug"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/rossigee/netbox-dns-operator/api/v1"
	"github.com/rossigee/netbox-dns-operator/controllers"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
	version  = "dev"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(v1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var webhookAddr string
	var webhookToken string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&webhookAddr, "webhook-bind-address", ":8082", "The address the webhook endpoint binds to.")
	flag.StringVar(&webhookToken, "webhook-token", "", "Token for webhook authentication (optional)")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Log version and build info
	buildInfo, ok := debug.ReadBuildInfo()
	setupLog.Info("Starting NetBox DNS Operator",
		"version", version,
		"goVersion", stdruntime.Version(),
		"os", stdruntime.GOOS,
		"arch", stdruntime.GOARCH,
		"buildInfo", buildInfo.String(),
		"buildOK", ok)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                server.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "netbox-dns-operator",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.NetBoxDNSOperatorReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "NetBoxDNSOperator")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	// Setup webhook server
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if webhookToken != "" {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer "+webhookToken {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
		defer func() { _ = r.Body.Close() }()
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		setupLog.Info("Received webhook", "payload", payload)
		// Trigger reconciliation for all operators
		client := mgr.GetClient()
		operators := &v1.NetBoxDNSOperatorList{}
		if err := client.List(context.Background(), operators); err != nil {
			setupLog.Error(err, "failed to list operators")
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		for _, op := range operators.Items {
			if op.Annotations == nil {
				op.Annotations = make(map[string]string)
			}
			op.Annotations["netbox-dns-operator/trigger"] = time.Now().Format(time.RFC3339)
			if err := client.Update(context.Background(), &op); err != nil {
				setupLog.Error(err, "failed to update operator for trigger", "operator", op.Name)
			}
		}
		w.WriteHeader(http.StatusOK)
	})
	server := &http.Server{Addr: webhookAddr, Handler: mux}

	// Add webhook server to manager for graceful shutdown
	if err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
		setupLog.Info("Starting webhook server", "address", webhookAddr)
		errChan := make(chan error, 1)
		go func() {
			errChan <- server.ListenAndServe()
		}()
		select {
		case err := <-errChan:
			return err
		case <-ctx.Done():
			setupLog.Info("Shutting down webhook server")
			return server.Shutdown(context.Background())
		}
	})); err != nil {
		setupLog.Error(err, "unable to add webhook server to manager")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
	setupLog.Info("manager stopped")
}
