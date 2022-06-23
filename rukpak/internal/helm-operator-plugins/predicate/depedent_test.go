package predicate

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestDependentPredicateFuncsCreate(t *testing.T) {
	for _, tt := range []struct {
		description string
		arg         event.CreateEvent
		result      bool
	}{
		{
			description: "Happy path - return false for Create event",
			arg: event.CreateEvent{
				Object: &unstructured.Unstructured{
					Object: map[string]interface{}{"key": "Value"},
				},
			},
			result: false,
		},
	} {
		t.Run(tt.description, func(t *testing.T) {
			funcs := DependentPredicateFuncs()
			result := funcs.CreateFunc(tt.arg)
			require.Equal(t, tt.result, result)
		})
	}
}

func TestDependentPredicateFuncsDelete(t *testing.T) {
	for _, tt := range []struct {
		description string
		arg         event.DeleteEvent
		result      bool
	}{
		{
			description: "Happy path - return true for Delete event",
			arg: event.DeleteEvent{
				Object: &unstructured.Unstructured{
					Object: map[string]interface{}{"key": "Value"},
				},
			},
			result: true,
		},
	} {
		t.Run(tt.description, func(t *testing.T) {
			funcs := DependentPredicateFuncs()
			result := funcs.DeleteFunc(tt.arg)
			require.Equal(t, tt.result, result)
		})
	}
}

func TestDependentPredicateFuncsGeneric(t *testing.T) {
	for _, tt := range []struct {
		description string
		arg         event.GenericEvent
		result      bool
	}{
		{
			description: "Happy path - return false for Generic event",
			arg: event.GenericEvent{
				Object: &unstructured.Unstructured{
					Object: map[string]interface{}{"key": "Value"},
				},
			},
			result: false,
		},
	} {
		t.Run(tt.description, func(t *testing.T) {
			funcs := DependentPredicateFuncs()
			result := funcs.GenericFunc(tt.arg)
			require.Equal(t, tt.result, result)
		})
	}
}

func TestDependentPredicateFuncsUpdate(t *testing.T) {
	for _, tt := range []struct {
		description string
		arg         event.UpdateEvent
		result      bool
	}{
		{
			description: "No update - return false",
			arg: event.UpdateEvent{
				ObjectOld: &unstructured.Unstructured{
					Object: map[string]interface{}{"key": "Value", "status": "statusValue"},
				},
				ObjectNew: &unstructured.Unstructured{
					Object: map[string]interface{}{"key": "Value", "status": "statusValue"},
				},
			},
			result: false,
		},
		{
			description: "No update with status difference - return false ignoring status differences",
			arg: event.UpdateEvent{
				ObjectOld: &unstructured.Unstructured{
					Object: map[string]interface{}{"key": "Value", "status": "oldstatusValue"},
				},
				ObjectNew: &unstructured.Unstructured{
					Object: map[string]interface{}{"key": "Value", "status": "newstatusValue"},
				},
			},
			result: false,
		},
		{
			description: "With update - return true",
			arg: event.UpdateEvent{
				ObjectOld: &unstructured.Unstructured{
					Object: map[string]interface{}{"key": "Value", "status": "statusValue"},
				},
				ObjectNew: &unstructured.Unstructured{
					Object: map[string]interface{}{"key": "Value1", "status": "statusValue"},
				},
			},
			result: true,
		},
	} {
		t.Run(tt.description, func(t *testing.T) {
			funcs := DependentPredicateFuncs()
			result := funcs.UpdateFunc(tt.arg)
			require.Equal(t, tt.result, result)
		})
	}
}
