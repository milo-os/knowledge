package v1alpha1

// This file implements minimal protobuf marshaling for our API types by wrapping
// JSON-encoded objects in a runtime.Unknown protobuf envelope. This satisfies the
// protobuf.Marshaler interface required by the kube-apiserver serializer without
// requiring generated .proto files, and prevents "does not implement protobuf
// marshalling interface" errors during namespace garbage collection.

import (
	"encoding/json"

	"k8s.io/apimachinery/pkg/runtime"
)

func marshalAsUnknown(obj interface{}) ([]byte, error) {
	raw, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	u := &runtime.Unknown{
		Raw:         raw,
		ContentType: runtime.ContentTypeJSON,
	}
	return u.Marshal()
}

func (r *RelationshipType) Marshal() ([]byte, error)     { return marshalAsUnknown(r) }
func (r *RelationshipTypeList) Marshal() ([]byte, error) { return marshalAsUnknown(r) }

func (r *RelationshipPolicy) Marshal() ([]byte, error)     { return marshalAsUnknown(r) }
func (r *RelationshipPolicyList) Marshal() ([]byte, error) { return marshalAsUnknown(r) }

func (r *ResourceRelationship) Marshal() ([]byte, error)     { return marshalAsUnknown(r) }
func (r *ResourceRelationshipList) Marshal() ([]byte, error) { return marshalAsUnknown(r) }

func (r *GraphQuery) Marshal() ([]byte, error)     { return marshalAsUnknown(r) }
func (r *GraphQueryList) Marshal() ([]byte, error) { return marshalAsUnknown(r) }
