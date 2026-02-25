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

package controllers

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/netbox-community/go-netbox/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	netboxv1 "github.com/rossigee/netbox-dns-operator/api/v1"
)

var (
	zoneRecordCount = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "netbox_dns_operator_zone_records",
			Help: "Number of DNS records in a zone",
		},
		[]string{"zone", "namespace", "name"},
	)
)

// NetBoxDNSOperatorReconciler reconciles a NetBoxDNSOperator object
type NetBoxDNSOperatorReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

//+kubebuilder:rbac:groups=netbox-dns-operator.rossigee.github.com,resources=netboxdnsoperators,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=netbox-dns-operator.rossigee.github.com,resources=netboxdnsoperators/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=netbox-dns-operator.rossigenerate.com,resources=netboxdnsoperators/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *NetBoxDNSOperatorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the NetBoxDNSOperator instance
	operator := &netboxv1.NetBoxDNSOperator{}
	err := r.Get(ctx, req.NamespacedName, operator)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			logger.Info("NetBoxDNSOperator resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		logger.Error(err, "Failed to get NetBoxDNSOperator")
		return ctrl.Result{}, err
	}

	logger.Info("Reconciling NetBoxDNSOperator", "name", operator.Name)

	// Fetch NetBox data
	devices, err := r.fetchNetBoxDevices(ctx, operator.Spec.NetBoxURL, operator.Spec.NetBoxToken)
	if err != nil {
		logger.Error(err, "Failed to fetch NetBox devices")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	ips, err := r.fetchNetBoxIPs(ctx, operator.Spec.NetBoxURL, operator.Spec.NetBoxToken)
	if err != nil {
		logger.Error(err, "Failed to fetch NetBox IPs")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	// Generate zone files
	zones := r.generateZoneFiles(operator.Spec.Zones, devices, ips)

	// Update ConfigMaps for each zone
	for zoneName, zoneData := range zones {
		if err := r.updateZoneConfigMap(ctx, zoneName, zoneData, operator); err != nil {
			logger.Error(err, "Failed to update ConfigMap", "zone", zoneName)
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
	}

	// Update operator status
	if err := r.updateOperatorStatus(ctx, operator, zones); err != nil {
		logger.Error(err, "Failed to update operator status")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	// Determine requeue interval
	requeueAfter := 5 * time.Minute // default
	if operator.Spec.ReloadInterval != "" {
		if parsed, err := time.ParseDuration(operator.Spec.ReloadInterval); err != nil {
			logger.Error(err, "Invalid reloadInterval, using default", "reloadInterval", operator.Spec.ReloadInterval)
		} else {
			requeueAfter = parsed
		}
	}

	logger.Info("Successfully reconciled NetBoxDNSOperator", "requeueAfter", requeueAfter)
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// fetchNetBoxDevices fetches device information from NetBox
func (r *NetBoxDNSOperatorReconciler) fetchNetBoxDevices(ctx context.Context, netboxURL, token string) ([]Device, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	c := netbox.NewAPIClientFor(netboxURL, token)

	devices := []Device{}

	// Fetch all devices
	res, _, err := c.DcimAPI.DcimDevicesList(ctxWithTimeout).Execute()
	if err != nil {
		return nil, fmt.Errorf("failed to list devices: %w", err)
	}

	for _, device := range res.Results {
		d := Device{}
		if device.Name.IsSet() {
			d.Name = *device.Name.Get()
		}
		if device.PrimaryIp4.IsSet() {
			if ip4 := device.PrimaryIp4.Get(); ip4 != nil {
				d.PrimaryIP = ip4.Address
			}
		} else if device.PrimaryIp6.IsSet() {
			if ip6 := device.PrimaryIp6.Get(); ip6 != nil {
				d.PrimaryIP = ip6.Address
			}
		}
		devices = append(devices, d)
	}

	return devices, nil
}

// fetchNetBoxIPs fetches IP address information from NetBox
func (r *NetBoxDNSOperatorReconciler) fetchNetBoxIPs(ctx context.Context, netboxURL, token string) ([]IPAddress, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	c := netbox.NewAPIClientFor(netboxURL, token)

	ips := []IPAddress{}

	// Fetch all IP addresses
	res, _, err := c.IpamAPI.IpamIpAddressesList(ctxWithTimeout).Execute()
	if err != nil {
		return nil, fmt.Errorf("failed to list IP addresses: %w", err)
	}

	for _, ip := range res.Results {
		i := IPAddress{
			Address: ip.Address,
		}
		if ip.DnsName != nil {
			i.DNSName = *ip.DnsName
		}
		ips = append(ips, i)
	}

	return ips, nil
}

// generateZoneFiles generates DNS zone files from NetBox data
func (r *NetBoxDNSOperatorReconciler) generateZoneFiles(zones []string, devices []Device, ips []IPAddress) map[string]string {
	zoneFiles := make(map[string]string)

	for _, zone := range zones {
		var records []string

		// Generate SOA record
		serial := time.Now().Format("2006010215") // YYYYMMDDHH format
		soa := fmt.Sprintf(`$TTL 300
@ IN SOA ns1.%s. admin.%s. (
    %s ; serial
    3600   ; refresh
    1800   ; retry
    604800 ; expire
    300    ; minimum
)
@ IN NS ns1.%s.`, zone, zone, serial, zone)
		records = append(records, soa)

		// Generate A records from devices
		for _, device := range devices {
			if strings.HasSuffix(device.Name, "."+zone) {
				shortName := strings.TrimSuffix(device.Name, "."+zone)
				records = append(records, fmt.Sprintf("%s IN A %s", shortName, device.PrimaryIP))
			}
		}

		// Generate PTR records for IPs
		for _, ip := range ips {
			if ip.DNSName != "" && strings.HasSuffix(ip.DNSName, "."+zone) {
				// Convert IP to reverse notation
				reverseIP := r.ipToReverse(ip.Address)
				records = append(records, fmt.Sprintf("%s IN PTR %s.", reverseIP, ip.DNSName))
			}
		}

		zoneFiles[zone] = strings.Join(records, "\n")
	}

	return zoneFiles
}

// ipToReverse converts an IP address to reverse DNS notation
func (r *NetBoxDNSOperatorReconciler) ipToReverse(ipStr string) string {
	ip := net.ParseIP(ipStr)
	if ip.To4() != nil {
		// IPv4
		parts := strings.Split(ipStr, ".")
		for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
			parts[i], parts[j] = parts[j], parts[i]
		}
		return strings.Join(parts, ".")
	}
	// IPv6
	bytes := ip.To16()
	var nibbles []string
	for i := len(bytes) - 1; i >= 0; i-- {
		nibbles = append(nibbles, fmt.Sprintf("%x", bytes[i]>>4))
		nibbles = append(nibbles, fmt.Sprintf("%x", bytes[i]&0xF))
	}
	return strings.Join(nibbles, ".")
}

// updateZoneConfigMap updates or creates a ConfigMap for a DNS zone
func (r *NetBoxDNSOperatorReconciler) updateZoneConfigMap(ctx context.Context, zoneName, zoneData string, operator *netboxv1.NetBoxDNSOperator) error {
	cmName := fmt.Sprintf("coredns-%s-zone", strings.ReplaceAll(zoneName, ".", "-"))
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: operator.Namespace,
			Labels: map[string]string{
				"app":                      "netbox-dns-operator",
				"netbox-dns-operator/zone": zoneName,
			},
		},
		Data: map[string]string{
			zoneName: zoneData,
		},
	}

	// Set owner reference so ConfigMap gets garbage collected
	if err := ctrl.SetControllerReference(operator, cm, r.Scheme); err != nil {
		return err
	}

	// Try to create or update
	existing := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: cmName, Namespace: operator.Namespace}, existing)
	if err != nil && errors.IsNotFound(err) {
		return r.Create(ctx, cm)
	} else if err != nil {
		return err
	}

	// Update existing ConfigMap
	existing.Data = cm.Data
	return r.Update(ctx, existing)
}

// updateOperatorStatus updates the status of the NetBoxDNSOperator
func (r *NetBoxDNSOperatorReconciler) updateOperatorStatus(ctx context.Context, operator *netboxv1.NetBoxDNSOperator, zones map[string]string) error {
	status := &netboxv1.NetBoxDNSOperatorStatus{
		LastSyncTime: &metav1.Time{Time: time.Now()},
		ZoneStatus:   make(map[string]netboxv1.ZoneStatus),
	}

	for zoneName, zoneData := range zones {
		recordCount := strings.Count(zoneData, " IN ") // Count DNS records
		serial := time.Now().Format("2006010215")

		status.ZoneStatus[zoneName] = netboxv1.ZoneStatus{
			RecordCount: recordCount,
			Serial:      serial,
			LastUpdate:  &metav1.Time{Time: time.Now()},
		}

		// Update metric
		zoneRecordCount.WithLabelValues(zoneName, operator.Namespace, operator.Name).Set(float64(recordCount))
	}

	operator.Status = *status
	return r.Status().Update(ctx, operator)
}

// SetupWithManager sets up the controller with the Manager.
func (r *NetBoxDNSOperatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&netboxv1.NetBoxDNSOperator{}).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}

// Data structures for NetBox API responses
type Device struct {
	Name      string
	PrimaryIP string
}

type IPAddress struct {
	Address string
	DNSName string
}
