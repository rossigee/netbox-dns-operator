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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NetBoxDNSOperatorSpec defines the desired state of NetBoxDNSOperator
type NetBoxDNSOperatorSpec struct {
	// NetBoxURL is the URL of the NetBox instance
	// +kubebuilder:validation:Format=uri
	NetBoxURL string `json:"netboxURL"`

	// NetBoxToken is the API token for NetBox authentication
	// +kubebuilder:validation:MinLength=1
	NetBoxToken string `json:"netboxToken"`

	// Zones is the list of DNS zones to manage
	// +kubebuilder:validation:MinItems=1
	Zones []string `json:"zones,omitempty"`

	// ReloadInterval is how often to check for NetBox changes (default: 5m)
	ReloadInterval string `json:"reloadInterval,omitempty"`

	// WebhookURL is the URL for NetBox webhooks (optional)
	// +kubebuilder:validation:Format=uri
	WebhookURL string `json:"webhookURL,omitempty"`
}

// NetBoxDNSOperatorStatus defines the observed state of NetBoxDNSOperator
type NetBoxDNSOperatorStatus struct {
	// LastSyncTime is the timestamp of the last successful sync
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// ZoneStatus contains status information for each zone
	ZoneStatus map[string]ZoneStatus `json:"zoneStatus,omitempty"`

	// Conditions represent the latest available observations of the operator's state
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ZoneStatus represents the status of a DNS zone
type ZoneStatus struct {
	// RecordCount is the number of DNS records in the zone
	RecordCount int `json:"recordCount"`

	// Serial is the current SOA serial number
	Serial string `json:"serial"`

	// LastUpdate is when this zone was last updated
	LastUpdate *metav1.Time `json:"lastUpdate,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// NetBoxDNSOperator is the Schema for the netboxdnsoperators API
type NetBoxDNSOperator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NetBoxDNSOperatorSpec   `json:"spec,omitempty"`
	Status NetBoxDNSOperatorStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// NetBoxDNSOperatorList contains a list of NetBoxDNSOperator
type NetBoxDNSOperatorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NetBoxDNSOperator `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NetBoxDNSOperator{}, &NetBoxDNSOperatorList{})
}
