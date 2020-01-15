package kubeletconfig

import (
	"fmt"

	"github.com/ghodss/yaml"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	testutils "github.com/openshift-kni/performance-addon-operators/pkg/utils/testing"
)

var _ = Describe("Kubelet Config", func() {
	It("should generate yaml with expected parameters", func() {
		profile := testutils.NewPerformanceProfile("test")
		kc, err := New(profile)
		Expect(err).ToNot(HaveOccurred())

		y, err := yaml.Marshal(kc)
		Expect(err).ToNot(HaveOccurred())

		manifest := string(y)

		selectorKey, selectorValue := testutils.GetFirstKeyAndValue(profile.Spec.MachineConfigPoolSelector)
		Expect(manifest).To(ContainSubstring(fmt.Sprintf("%s: %s", selectorKey, selectorValue)))
		Expect(manifest).To(ContainSubstring("reservedSystemCPUs: 0-3"))
		Expect(manifest).To(ContainSubstring("topologyManagerPolicy: best-effort"))
		Expect(manifest).To(ContainSubstring("cpuManagerPolicy: static"))
	})
})
