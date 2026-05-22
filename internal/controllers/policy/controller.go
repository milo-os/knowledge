package policy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	celengine "go.miloapis.com/knowledge/internal/cel"
	knowledgev1alpha1 "go.miloapis.com/knowledge/pkg/apis/knowledge/v1alpha1"
)

const (
	// LabelCreatedByPolicy marks ResourceRelationships managed by a policy.
	LabelCreatedByPolicy = "knowledge.miloapis.com/created-by-policy"
	// LabelPolicyName carries the owning policy's name.
	LabelPolicyName = "knowledge.miloapis.com/policy-name"
	// LabelPolicyNamespace carries the owning policy's namespace.
	LabelPolicyNamespace = "knowledge.miloapis.com/policy-namespace"

	conditionTypeReady = "Ready"
	reasonReconciled   = "Reconciled"
	reasonError        = "ReconcileError"
)

// Reconciler reconciles RelationshipPolicy objects and synchronizes the
// ResourceRelationship objects they manage.
type Reconciler struct {
	client.Client
	DynamicClient dynamic.Interface
	RESTMapper    *restmapper.DeferredDiscoveryRESTMapper
	CEL           *celengine.Engine
}

// SetupWithManager registers the reconciler with the manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&knowledgev1alpha1.RelationshipPolicy{}).
		// Watch resource types used as subjects or objects by active policies so
		// that relationship creation/deletion happens immediately on resource changes.
		Watches(&corev1.Service{}, handler.EnqueueRequestsFromMapFunc(r.mapObjectToPolicies)).
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(r.mapObjectToPolicies)).
		Watches(&appsv1.Deployment{}, handler.EnqueueRequestsFromMapFunc(r.mapObjectToPolicies)).
		Watches(&appsv1.ReplicaSet{}, handler.EnqueueRequestsFromMapFunc(r.mapObjectToPolicies)).
		Watches(&networkingv1.Ingress{}, handler.EnqueueRequestsFromMapFunc(r.mapObjectToPolicies)).
		Complete(r)
}

// mapObjectToPolicies enqueues all RelationshipPolicies in the same namespace
// as the changed object so they reconcile immediately on endpoint changes.
func (r *Reconciler) mapObjectToPolicies(ctx context.Context, obj client.Object) []reconcile.Request {
	policies := &knowledgev1alpha1.RelationshipPolicyList{}
	if err := r.List(ctx, policies, client.InNamespace(obj.GetNamespace())); err != nil {
		return nil
	}
	reqs := make([]reconcile.Request, len(policies.Items))
	for i := range policies.Items {
		reqs[i] = reconcile.Request{NamespacedName: types.NamespacedName{
			Namespace: policies.Items[i].Namespace,
			Name:      policies.Items[i].Name,
		}}
	}
	return reqs
}

// Reconcile implements reconcile.Reconciler.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("policy", req.NamespacedName)

	policy := &knowledgev1alpha1.RelationshipPolicy{}
	if err := r.Get(ctx, req.NamespacedName, policy); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	count, err := r.reconcileEdges(ctx, policy)

	base := policy.DeepCopy()
	if err != nil {
		logger.Error(err, "failed to reconcile edges")
		r.setReadyCondition(policy, metav1.ConditionFalse, reasonError, err.Error())
	} else {
		policy.Status.ObservedGeneration = policy.Generation
		policy.Status.DiscoveredEdgesCount = int64(count)
		r.setReadyCondition(policy, metav1.ConditionTrue, reasonReconciled, fmt.Sprintf("discovered %d edges", count))
	}

	if patchErr := r.Status().Patch(ctx, policy, client.MergeFrom(base)); patchErr != nil {
		logger.Error(patchErr, "failed to patch status")
		if err == nil {
			return ctrl.Result{}, patchErr
		}
	}

	if err != nil {
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}

	// Periodically requeue to pick up source-resource changes since we don't
	// yet have dynamic informers wired for the subject GVK.
	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

func (r *Reconciler) reconcileEdges(ctx context.Context, policy *knowledgev1alpha1.RelationshipPolicy) (int, error) {
	gvr, err := r.subjectGVR(policy.Spec.Subject)
	if err != nil {
		return 0, fmt.Errorf("resolve subject GVR: %w", err)
	}

	sources, err := r.listSources(ctx, gvr, policy.Spec.Subject.Namespaces)
	if err != nil {
		return 0, fmt.Errorf("list subject resources: %w", err)
	}

	candidates, err := r.listCandidates(ctx, policy)
	if err != nil {
		return 0, fmt.Errorf("list candidates: %w", err)
	}

	// Index candidates by (namespace, name) for fast existence checks.
	// All candidates from listCandidates are the same kind, so kind is not needed as a key.
	// Dynamic client list responses omit kind/apiVersion from individual item bodies,
	// making obj.GetKind() return "" — using namespace+name is both correct and sufficient.
	type objKey struct{ namespace, name string }
	existingObjects := make(map[objKey]bool, len(candidates))
	for _, c := range candidates {
		obj := unstructured.Unstructured{Object: c}
		existingObjects[objKey{namespace: obj.GetNamespace(), name: obj.GetName()}] = true
	}

	desired := map[string]*knowledgev1alpha1.ResourceRelationship{}
	for _, src := range sources {
		refs, err := r.CEL.Evaluate(ctx, policy.Spec.Discovery.Expression, src.Object, candidates)
		if err != nil {
			return 0, fmt.Errorf("CEL evaluation for %s/%s: %w", src.GetNamespace(), src.GetName(), err)
		}
		for _, refMap := range refs {
			// Skip edges whose target object doesn't currently exist.
			if len(existingObjects) > 0 {
				k := objKey{
					namespace: stringFrom(refMap, "namespace"),
					name:      stringFrom(refMap, "name"),
				}
				if !existingObjects[k] {
					continue
				}
			}
			rel, err := r.buildRelationship(policy, &src, refMap)
			if err != nil {
				return 0, err
			}
			desired[rel.Name] = rel
		}
	}

	existing := &knowledgev1alpha1.ResourceRelationshipList{}
	if err := r.List(ctx, existing,
		client.InNamespace(policy.Namespace),
		client.MatchingLabels{
			LabelCreatedByPolicy: "true",
			LabelPolicyName:      policy.Name,
			LabelPolicyNamespace: policy.Namespace,
		},
	); err != nil {
		return 0, fmt.Errorf("list existing relationships: %w", err)
	}

	existingByName := make(map[string]knowledgev1alpha1.ResourceRelationship, len(existing.Items))
	for _, e := range existing.Items {
		existingByName[e.Name] = e
	}

	for name, rel := range desired {
		if _, ok := existingByName[name]; ok {
			continue
		}
		if err := r.Create(ctx, rel); err != nil && !apierrors.IsAlreadyExists(err) {
			return 0, fmt.Errorf("create relationship %q: %w", name, err)
		}
	}

	for name, e := range existingByName {
		if _, ok := desired[name]; ok {
			continue
		}
		toDelete := e
		if err := r.Delete(ctx, &toDelete); err != nil && !apierrors.IsNotFound(err) {
			return 0, fmt.Errorf("delete stale relationship %q: %w", name, err)
		}
	}

	return len(desired), nil
}

// listCandidates fetches all objects of the RelationshipType's objectGVK so that
// CEL expressions can filter them via the `candidates` variable.
func (r *Reconciler) listCandidates(ctx context.Context, policy *knowledgev1alpha1.RelationshipPolicy) ([]map[string]interface{}, error) {
	rt := &knowledgev1alpha1.RelationshipType{}
	if err := r.Get(ctx, client.ObjectKey{Name: policy.Spec.RelationshipType.Name}, rt); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get RelationshipType %q: %w", policy.Spec.RelationshipType.Name, err)
	}
	gk := schema.GroupKind{Group: rt.Spec.ObjectGVK.Group, Kind: rt.Spec.ObjectGVK.Kind}
	mapping, err := r.RESTMapper.RESTMapping(gk, rt.Spec.ObjectGVK.Version)
	if err != nil {
		return nil, fmt.Errorf("resolve object GVR for candidates: %w", err)
	}
	var items []unstructured.Unstructured
	if len(policy.Spec.Subject.Namespaces) == 0 {
		list, err := r.DynamicClient.Resource(mapping.Resource).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list candidates for %s: %w", rt.Spec.ObjectGVK.Kind, err)
		}
		items = list.Items
	} else {
		for _, ns := range policy.Spec.Subject.Namespaces {
			list, err := r.DynamicClient.Resource(mapping.Resource).Namespace(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				return nil, fmt.Errorf("list candidates for %s in ns %s: %w", rt.Spec.ObjectGVK.Kind, ns, err)
			}
			items = append(items, list.Items...)
		}
	}
	result := make([]map[string]interface{}, len(items))
	for i, item := range items {
		result[i] = item.Object
	}
	return result, nil
}

func (r *Reconciler) subjectGVR(s knowledgev1alpha1.SubjectSelector) (schema.GroupVersionResource, error) {
	gk := schema.GroupKind{Group: s.APIGroup, Kind: s.Kind}
	mapping, err := r.RESTMapper.RESTMapping(gk)
	if err != nil {
		return schema.GroupVersionResource{}, err
	}
	return mapping.Resource, nil
}

func (r *Reconciler) listSources(ctx context.Context, gvr schema.GroupVersionResource, namespaces []string) ([]unstructured.Unstructured, error) {
	if len(namespaces) == 0 {
		list, err := r.DynamicClient.Resource(gvr).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		return list.Items, nil
	}
	var all []unstructured.Unstructured
	for _, ns := range namespaces {
		list, err := r.DynamicClient.Resource(gvr).Namespace(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list in namespace %q: %w", ns, err)
		}
		all = append(all, list.Items...)
	}
	return all, nil
}

func (r *Reconciler) buildRelationship(policy *knowledgev1alpha1.RelationshipPolicy, src *unstructured.Unstructured, refMap map[string]interface{}) (*knowledgev1alpha1.ResourceRelationship, error) {
	objAPIGroup := stringFrom(refMap, "apiGroup")
	objKind := stringFrom(refMap, "kind")
	objName := stringFrom(refMap, "name")
	if objKind == "" || objName == "" {
		return nil, fmt.Errorf("CEL result missing kind/name (got %v)", refMap)
	}
	objNamespace := stringFrom(refMap, "namespace")
	objCtxKind := stringFrom(refMap, "controlPlaneContextKind")
	objCtxName := stringFrom(refMap, "controlPlaneContextName")
	if objCtxKind == "" {
		objCtxKind = policy.Spec.ControlPlaneContextRef.Kind
	}
	if objCtxName == "" {
		objCtxName = policy.Spec.ControlPlaneContextRef.Name
	}

	subjectGV, _ := schema.ParseGroupVersion(src.GetAPIVersion())
	subject := knowledgev1alpha1.ResourceEndpoint{
		APIGroup:               subjectGV.Group,
		Kind:                   src.GetKind(),
		Name:                   src.GetName(),
		Namespace:              src.GetNamespace(),
		ControlPlaneContextRef: policy.Spec.ControlPlaneContextRef,
	}
	object := knowledgev1alpha1.ResourceEndpoint{
		APIGroup:               objAPIGroup,
		Kind:                   objKind,
		Name:                   objName,
		Namespace:              objNamespace,
		ControlPlaneContextRef: knowledgev1alpha1.ControlPlaneContextRef{Kind: objCtxKind, Name: objCtxName},
	}

	name := deterministicName(policy, subject, object)
	isController := true
	blockOwnerDeletion := true
	rel := &knowledgev1alpha1.ResourceRelationship{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: policy.Namespace,
			Labels: map[string]string{
				LabelCreatedByPolicy: "true",
				LabelPolicyName:      policy.Name,
				LabelPolicyNamespace: policy.Namespace,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         knowledgev1alpha1.GroupVersion.String(),
					Kind:               "RelationshipPolicy",
					Name:               policy.Name,
					UID:                policy.UID,
					Controller:         &isController,
					BlockOwnerDeletion: &blockOwnerDeletion,
				},
			},
		},
		Spec: knowledgev1alpha1.ResourceRelationshipSpec{
			RelationshipType: policy.Spec.RelationshipType,
			Subject:          subject,
			Object:           object,
			Source: knowledgev1alpha1.RelationshipSource{
				Type: "Policy",
				PolicyRef: &corev1.ObjectReference{
					APIVersion: knowledgev1alpha1.GroupVersion.String(),
					Kind:       "RelationshipPolicy",
					Namespace:  policy.Namespace,
					Name:       policy.Name,
					UID:        policy.UID,
				},
			},
		},
	}
	return rel, nil
}

func stringFrom(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// deterministicName produces a stable, dns-safe name for a relationship.
func deterministicName(policy *knowledgev1alpha1.RelationshipPolicy, subj, obj knowledgev1alpha1.ResourceEndpoint) string {
	key := strings.Join([]string{
		policy.Namespace, policy.Name,
		policy.Spec.RelationshipType.Name,
		subj.ControlPlaneContextRef.Kind, subj.ControlPlaneContextRef.Name, subj.APIGroup, subj.Kind, subj.Namespace, subj.Name,
		obj.ControlPlaneContextRef.Kind, obj.ControlPlaneContextRef.Name, obj.APIGroup, obj.Kind, obj.Namespace, obj.Name,
	}, "|")
	sum := sha256.Sum256([]byte(key))
	return "rp-" + hex.EncodeToString(sum[:8])
}

func (r *Reconciler) setReadyCondition(policy *knowledgev1alpha1.RelationshipPolicy, status metav1.ConditionStatus, reason, message string) {
	cond := metav1.Condition{
		Type:               conditionTypeReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: policy.Generation,
		LastTransitionTime: metav1.NewTime(time.Now()),
	}
	for i, existing := range policy.Status.Conditions {
		if existing.Type == cond.Type {
			if existing.Status == cond.Status {
				cond.LastTransitionTime = existing.LastTransitionTime
			}
			policy.Status.Conditions[i] = cond
			return
		}
	}
	policy.Status.Conditions = append(policy.Status.Conditions, cond)
}

