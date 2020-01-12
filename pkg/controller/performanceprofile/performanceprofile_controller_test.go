package performanceprofile

import (
	"context"
	"time"

	"github.com/openshift-kni/performance-addon-operators/pkg/controller/performanceprofile/components/featuregate"
	"github.com/openshift-kni/performance-addon-operators/pkg/controller/performanceprofile/components/tuned"

	"github.com/openshift-kni/performance-addon-operators/pkg/controller/performanceprofile/components/kubeletconfig"

	"github.com/openshift-kni/performance-addon-operators/pkg/controller/performanceprofile/components/machineconfig"

	"github.com/openshift-kni/performance-addon-operators/pkg/controller/performanceprofile/components/machineconfigpool"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	performancev1alpha1 "github.com/openshift-kni/performance-addon-operators/pkg/apis/performance/v1alpha1"
	"github.com/openshift-kni/performance-addon-operators/pkg/controller/performanceprofile/components"
	testutils "github.com/openshift-kni/performance-addon-operators/pkg/utils/testing"
	configv1 "github.com/openshift/api/config/v1"
	tunedv1 "github.com/openshift/cluster-node-tuning-operator/pkg/apis/tuned/v1"
	mcov1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const assetsDir = "../../../build/assets"

var _ = Describe("Controller", func() {
	var request reconcile.Request
	var profile *performancev1alpha1.PerformanceProfile

	BeforeEach(func() {
		profile = testutils.NewPerformanceProfile("test")
		request = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: metav1.NamespaceNone,
				Name:      profile.Name,
			},
		}
	})

	It("should add finalizer to the performance profile", func() {
		r := newFakeReconciler(profile)

		result, err := r.Reconcile(request)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal(reconcile.Result{}))

		updatedProfile := &performancev1alpha1.PerformanceProfile{}
		key := types.NamespacedName{
			Name:      profile.Name,
			Namespace: metav1.NamespaceNone,
		}
		Expect(r.client.Get(context.TODO(), key, updatedProfile)).ToNot(HaveOccurred())
		Expect(hasFinalizer(updatedProfile, finalizer)).To(Equal(true))
	})

	Context("with profile with finalizer", func() {
		BeforeEach(func() {
			profile.Finalizers = append(profile.Finalizers, finalizer)
		})

		It("should verify scripts required parameters", func() {
			profile.Spec.CPU.Isolated = nil
			r := newFakeReconciler(profile)

			// we do not return error, because we do not want to reconcile again, and just print error under the log,
			// once we will have validation webhook, this test will not be relevant anymore
			result, err := r.Reconcile(request)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			// verify that no components created by the controller
			mcp := &mcov1.MachineConfigPool{}
			key := types.NamespacedName{
				Name:      components.GetComponentName(profile.Name, components.RoleWorkerPerformance),
				Namespace: metav1.NamespaceNone,
			}
			err = r.client.Get(context.TODO(), key, mcp)
			Expect(errors.IsNotFound(err)).To(Equal(true))
		})

		It("should create and pause machine config pool of first reconcile loop", func() {
			r := newFakeReconciler(profile)

			result, err := r.Reconcile(request)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{RequeueAfter: 10 * time.Second}))

			name := components.GetComponentName(profile.Name, components.RoleWorkerPerformance)
			key := types.NamespacedName{
				Name:      name,
				Namespace: metav1.NamespaceNone,
			}

			// verify MachineConfigPool creation
			mcp := &mcov1.MachineConfigPool{}
			err = r.client.Get(context.TODO(), key, mcp)
			Expect(err).ToNot(HaveOccurred())

			// verify MachineConfigPool paused field
			Expect(mcp.Spec.Paused).To(BeTrue())
		})

		It("should create all other resources except KubeletConfig on second reconcile loop", func() {
			r := newFakeReconciler(profile)

			result, err := r.Reconcile(request)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{RequeueAfter: 10 * time.Second}))

			result, err = r.Reconcile(request)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{RequeueAfter: 10 * time.Second}))

			name := components.GetComponentName(profile.Name, components.RoleWorkerPerformance)
			key := types.NamespacedName{
				Name:      name,
				Namespace: metav1.NamespaceNone,
			}

			// verify MachineConfig creation
			mc := &mcov1.MachineConfig{}
			err = r.client.Get(context.TODO(), key, mc)
			Expect(err).ToNot(HaveOccurred())

			// verify that KubeletConfig wasn't created
			kc := &mcov1.KubeletConfig{}
			err = r.client.Get(context.TODO(), key, kc)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			// verify FeatureGate creation
			fg := &configv1.FeatureGate{}
			key.Name = components.FeatureGateLatencySensetiveName
			err = r.client.Get(context.TODO(), key, fg)
			Expect(err).ToNot(HaveOccurred())

			// verify tuned LatencySensitive creation
			tunedLatency := &tunedv1.Tuned{}
			key.Name = components.ProfileNameNetworkLatency
			key.Namespace = components.NamespaceNodeTuningOperator
			err = r.client.Get(context.TODO(), key, tunedLatency)
			Expect(err).ToNot(HaveOccurred())

			// verify tuned tuned real-time kernel creation
			tunedRTKernel := &tunedv1.Tuned{}
			key.Name = components.GetComponentName(profile.Name, components.ProfileNameWorkerRT)
			err = r.client.Get(context.TODO(), key, tunedRTKernel)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should create KubeletConfig on third reconcile loop", func() {
			r := newFakeReconciler(profile)

			result, err := r.Reconcile(request)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{RequeueAfter: 10 * time.Second}))

			result, err = r.Reconcile(request)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{RequeueAfter: 10 * time.Second}))

			result, err = r.Reconcile(request)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{RequeueAfter: 10 * time.Second}))

			name := components.GetComponentName(profile.Name, components.RoleWorkerPerformance)
			key := types.NamespacedName{
				Name:      name,
				Namespace: metav1.NamespaceNone,
			}

			// verify KubeletConfig creation
			kc := &mcov1.KubeletConfig{}
			err = r.client.Get(context.TODO(), key, kc)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should unpause machine config pool on fourth reconcile loop", func() {
			r := newFakeReconciler(profile)

			result, err := r.Reconcile(request)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{RequeueAfter: 10 * time.Second}))

			result, err = r.Reconcile(request)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{RequeueAfter: 10 * time.Second}))

			result, err = r.Reconcile(request)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{RequeueAfter: 10 * time.Second}))

			result, err = r.Reconcile(request)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{RequeueAfter: 10 * time.Second}))

			name := components.GetComponentName(profile.Name, components.RoleWorkerPerformance)
			key := types.NamespacedName{
				Name:      name,
				Namespace: metav1.NamespaceNone,
			}
			mcp := &mcov1.MachineConfigPool{}
			err = r.client.Get(context.TODO(), key, mcp)
			Expect(err).ToNot(HaveOccurred())

			// verify MachineConfigPool paused field
			Expect(mcp.Spec.Paused).To(BeFalse())
		})

		It("should do nothing on fifth reconcile loop", func() {
			r := newFakeReconciler(profile)

			result, err := r.Reconcile(request)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{RequeueAfter: 10 * time.Second}))

			result, err = r.Reconcile(request)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{RequeueAfter: 10 * time.Second}))

			result, err = r.Reconcile(request)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{RequeueAfter: 10 * time.Second}))

			result, err = r.Reconcile(request)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{RequeueAfter: 10 * time.Second}))

			result, err = r.Reconcile(request)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
		})

		Context("with existing MachineConfigPool", func() {
			var mcp *mcov1.MachineConfigPool

			BeforeEach(func() {
				mcp = machineconfigpool.New(profile)
			})

			It("should pause on first reconcile loop", func() {
				r := newFakeReconciler(profile, mcp)
				result, err := r.Reconcile(request)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).To(Equal(reconcile.Result{RequeueAfter: 10 * time.Second}))
			})
		})
	})

	Context("with profile with deletion timestamp", func() {
		BeforeEach(func() {
			profile.DeletionTimestamp = &metav1.Time{
				Time: time.Now(),
			}
			profile.Finalizers = append(profile.Finalizers, finalizer)
		})

		It("should pause machine config pool of first reconcile loop", func() {
			mcp := machineconfigpool.New(profile)

			r := newFakeReconciler(profile, mcp)
			result, err := r.Reconcile(request)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{RequeueAfter: 10 * time.Second}))

			name := components.GetComponentName(profile.Name, components.RoleWorkerPerformance)
			key := types.NamespacedName{
				Name:      name,
				Namespace: metav1.NamespaceNone,
			}

			mcpUpdated := &mcov1.MachineConfigPool{}
			err = r.client.Get(context.TODO(), key, mcpUpdated)
			Expect(err).ToNot(HaveOccurred())

			// verify MachineConfigPool paused field
			Expect(mcpUpdated.Spec.Paused).To(BeTrue())
		})

		It("should remove all components and remove the finalizer on second reconcile loop", func() {

			mcp := machineconfigpool.New(profile)

			mc, err := machineconfig.New(assetsDir, profile)
			Expect(err).ToNot(HaveOccurred())

			kc, err := kubeletconfig.New(profile)
			Expect(err).ToNot(HaveOccurred())

			fg := featuregate.NewLatencySensitive()

			tunedLatency, err := tuned.NewNetworkLatency(assetsDir)
			Expect(err).ToNot(HaveOccurred())

			tunedRTKernel, err := tuned.NewWorkerRealTimeKernel(assetsDir, profile)
			Expect(err).ToNot(HaveOccurred())

			r := newFakeReconciler(profile, mcp, mc, kc, fg, tunedLatency, tunedRTKernel)
			result, err := r.Reconcile(request)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{RequeueAfter: 10 * time.Second}))

			result, err = r.Reconcile(request)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			// verify that controller deleted all components
			name := components.GetComponentName(profile.Name, components.RoleWorkerPerformance)
			key := types.NamespacedName{
				Name:      name,
				Namespace: metav1.NamespaceNone,
			}

			// verify MachineConfigPool deletion
			err = r.client.Get(context.TODO(), key, mcp)
			Expect(errors.IsNotFound(err)).To(Equal(true))

			// verify MachineConfig deletion
			err = r.client.Get(context.TODO(), key, mc)
			Expect(errors.IsNotFound(err)).To(Equal(true))

			// verify KubeletConfig deletion
			err = r.client.Get(context.TODO(), key, kc)
			Expect(errors.IsNotFound(err)).To(Equal(true))

			// verify feature gate deletion
			// TOOD: uncomment once https://bugzilla.redhat.com/show_bug.cgi?id=1788061 fixed
			// key.Name = components.FeatureGateLatencySensetiveName
			// err = r.client.Get(context.TODO(), key, fg)
			// Expect(errors.IsNotFound(err)).To(Equal(true))

			// verify tuned latency deletion
			key.Name = components.ProfileNameNetworkLatency
			key.Namespace = components.NamespaceNodeTuningOperator
			err = r.client.Get(context.TODO(), key, tunedLatency)
			Expect(errors.IsNotFound(err)).To(Equal(true))

			// verify tuned real-time kernel deletion
			key.Name = components.GetComponentName(profile.Name, components.ProfileNameWorkerRT)
			key.Namespace = components.NamespaceNodeTuningOperator
			err = r.client.Get(context.TODO(), key, tunedRTKernel)
			Expect(errors.IsNotFound(err)).To(Equal(true))

			// verify finalizer deletion
			key.Name = profile.Name
			key.Namespace = metav1.NamespaceNone
			updatedProfile := &performancev1alpha1.PerformanceProfile{}
			Expect(r.client.Get(context.TODO(), key, updatedProfile)).ToNot(HaveOccurred())
			Expect(hasFinalizer(updatedProfile, finalizer)).To(Equal(false))
		})
	})
})

// newFakeReconciler returns a new reconcile.Reconciler with a fake client
func newFakeReconciler(initObjects ...runtime.Object) *ReconcilePerformanceProfile {
	fakeClient := fake.NewFakeClient(initObjects...)
	return &ReconcilePerformanceProfile{
		client:    fakeClient,
		scheme:    scheme.Scheme,
		assetsDir: assetsDir,
	}
}