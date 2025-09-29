package crdupgradesafety

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConciseUnhandledMessage_NoPrefix(t *testing.T) {
	raw := "some other error"
	require.Equal(t, raw, conciseUnhandledMessage(raw))
}

func TestConciseUnhandledMessage_SingleChange(t *testing.T) {
	raw := "unhandled changes found :\n- Format: \"\"\n+ Format: \"email\"\n"
	require.Equal(t, "unhandled changes found (Format \"\" -> \"email\")", conciseUnhandledMessage(raw))
}

func TestConciseUnhandledMessage_MultipleChanges(t *testing.T) {
	raw := "unhandled changes found :\n- Format: \"\"\n+ Format: \"email\"\n- Default: nil\n+ Default: \"value\"\n- Title: \"\"\n+ Title: \"Widget\"\n- Description: \"old\"\n+ Description: \"new\"\n"
	got := conciseUnhandledMessage(raw)
	require.Equal(t, "unhandled changes found (Format \"\" -> \"email\"; Default nil -> \"value\"; Title \"\" -> \"Widget\"; Description \"old\" -> \"new\")", got)
}

func TestConciseUnhandledMessage_SkipComplexValues(t *testing.T) {
	raw := "unhandled changes found :\n- Default: &v1.JSONSchemaProps{}\n+ Default: &v1.JSONSchemaProps{Type: \"string\"}\n"
	require.Equal(t, "unhandled changes found (Default <complex value> -> <complex value> (changed))", conciseUnhandledMessage(raw))
}
