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
	"strings"
	"testing"
)

func TestIpToReverse(t *testing.T) {
	r := &NetBoxDNSOperatorReconciler{}

	tests := []struct {
		input    string
		expected string
	}{
		{"192.168.1.1", "1.1.168.192"},
		{"10.0.0.1", "1.0.0.10"},
		{"2001:db8::1", "0.1.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.b.8.0.d.0.1.2.0"},
	}

	for _, test := range tests {
		result := r.ipToReverse(test.input)
		if result != test.expected {
			t.Errorf("ipToReverse(%s) = %s; expected %s", test.input, result, test.expected)
		}
	}
}

func TestGenerateZoneFiles(t *testing.T) {
	r := &NetBoxDNSOperatorReconciler{}

	devices := []Device{
		{Name: "server1.example.com", PrimaryIP: "192.168.1.1"},
		{Name: "server2.example.com", PrimaryIP: "192.168.1.2"},
	}

	ips := []IPAddress{
		{Address: "192.168.1.1", DNSName: "server1.example.com"},
		{Address: "192.168.1.2", DNSName: "server2.example.com"},
	}

	zones := []string{"example.com"}

	result := r.generateZoneFiles(zones, devices, ips)

	if len(result) != 1 {
		t.Errorf("Expected 1 zone, got %d", len(result))
	}

	zoneData, exists := result["example.com"]
	if !exists {
		t.Error("Zone example.com not found")
	}

	// Check if SOA is present
	if len(zoneData) == 0 {
		t.Error("Zone data is empty")
	}

	// Check for A records
	if !strings.Contains(zoneData, "server1 IN A 192.168.1.1") {
		t.Error("A record for server1 not found")
	}

	// Check for PTR records
	if !strings.Contains(zoneData, "1.1.168.192 IN PTR server1.example.com.") {
		t.Error("PTR record for server1 not found")
	}
}
