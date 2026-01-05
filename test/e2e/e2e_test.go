/*
Copyright 2026 The Butler Authors.

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

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
)

var _ = Describe("MachineRequest Controller", Ordered, func() {
	BeforeAll(func() {
		// Setup code before all tests
	})

	AfterAll(func() {
		// Cleanup code after all tests
	})

	Context("When creating a MachineRequest for Nutanix", func() {
		It("should create a VM in Nutanix", func() {
			Skip("E2E tests require Nutanix environment")
		})

		It("should wait for IP address", func() {
			Skip("E2E tests require Nutanix environment")
		})

		It("should transition to Running phase", func() {
			Skip("E2E tests require Nutanix environment")
		})
	})

	Context("When deleting a MachineRequest", func() {
		It("should delete the VM from Nutanix", func() {
			Skip("E2E tests require Nutanix environment")
		})
	})
})
