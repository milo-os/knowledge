package relationship

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	knowledgev1alpha1 "go.miloapis.com/knowledge/pkg/apis/knowledge/v1alpha1"
)

const (
	conditionTypeValid       = "Valid"
	reasonEndpointsExist     = "EndpointsExist"
	reasonEndpointNotFound   = "EndpointNotFound"
	reasonEndpointLookupFail = "EndpointLookupFailed"

	requeueInterval = 10 * time.Second
)

// Reconciler verifies that both endpoints of a ResourceRelationship still
// resolve to live resources and updates the Valid condition accordingly.
type Reconciler struct {
	client.Client
	DynamicClient dynamic.Interface
	RESTMapper    *restmapper.DeferredDiscoveryRESTMapper
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&knowledgev1alpha1.ResourceRelationship{}).
		Complete(r)
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	rel := &knowledgev1alpha1.ResourceRelationship{}
	if err := r.Get(ctx, req.NamespacedName, rel); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	base := rel.DeepCopy()
	status, reason, message := r.checkEndpoints(ctx, rel)
	rel.Status.ObservedGeneration = rel.Generation
	r.setCondition(rel, status, reason, message)
	if err := r.Status().Patch(ctx, rel, client.MergeFrom(base)); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: requeueInterval + time.Duration(rand.Intn(60))*time.Second}, nil
}

func (r *Reconciler) checkEndpoints(ctx context.Context, rel *knowledgev1alpha1.ResourceRelationship) (metav1.ConditionStatus, string, string) {
	if err := r.checkEndpoint(ctx, rel.Spec.Subject); err != nil {
		return metav1.ConditionFalse, classify(err), fmt.Sprintf("subject endpoint: %v", err)
	}
	if err := r.checkEndpoint(ctx, rel.Spec.Object); err != nil {
		return metav1.ConditionFalse, classify(err), fmt.Sprintf("object endpoint: %v", err)
	}
	return metav1.ConditionTrue, reasonEndpointsExist, "both endpoints exist"
}

func classify(err error) string {
	if apierrors.IsNotFound(err) {
		return reasonEndpointNotFound
	}
	return reasonEndpointLookupFail
}

func (r *Reconciler) checkEndpoint(ctx context.Context, ep knowledgev1alpha1.ResourceEndpoint) error {
	mapping, err := r.RESTMapper.RESTMapping(schema.GroupKind{Group: ep.APIGroup, Kind: ep.Kind})
	if err != nil {
		return fmt.Errorf("REST mapping for %s/%s: %w", ep.APIGroup, ep.Kind, err)
	}
	res := r.DynamicClient.Resource(mapping.Resource)
	if ep.Namespace != "" {
		_, err = res.Namespace(ep.Namespace).Get(ctx, ep.Name, metav1.GetOptions{})
	} else {
		_, err = res.Get(ctx, ep.Name, metav1.GetOptions{})
	}
	return err
}

func (r *Reconciler) setCondition(rel *knowledgev1alpha1.ResourceRelationship, status metav1.ConditionStatus, reason, message string) {
	cond := metav1.Condition{
		Type:               conditionTypeValid,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: rel.Generation,
		LastTransitionTime: metav1.NewTime(time.Now()),
	}
	for i, existing := range rel.Status.Conditions {
		if existing.Type == cond.Type {
			if existing.Status == cond.Status {
				cond.LastTransitionTime = existing.LastTransitionTime
			}
			rel.Status.Conditions[i] = cond
			return
		}
	}
	rel.Status.Conditions = append(rel.Status.Conditions, cond)
}
