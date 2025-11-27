# Guide to Operator-Controller CRD extensions

All operator-controller (`opcon` for short) extensions to CRDs are part of the
comments to fields within the APIs. The fields look like XML tags, to distinguish
them from kubebuilder tags.

All tags start with `<opcon:`, end with `>` and have additional fields in between.
Usually the second field is `experimental`. Some tags may have an end tag (like XML)
that starts with `</`.

## Experimental Field

* Tag: `<opcon:experimental>`

The field that follows is experimental, and is not included in the standard CRD. It *is* included
in the experimental CRD.

## Experimental Validation

* Tag: `<opcon:standard:validation:VALIDATION>`
* Tag: `<opcon:experimental:validation:VALIDATION>`

A standard and/or experimental validation which may differ from one another. For example, where the
experimental CRD has extra enumerations.

Where `VALIDATION` is one of:

* `Enum=list;of;enums`

A semi-colon separated list of enumerations, similar to the `+kubebuilder:validation:Enum` scheme.

* `XValidation:message="something",rule="something"`

An XValidation scheme, similar to the `+kubebuilder:validation:XValidation` scheme, but more limited.

* `Optional`

Indicating that this field should not be listed as required in its parent.

* `Required`

Indicating that this field should be listed as required in its parent.

## Experimental Description

* Start Tag: `<opcon:experimental:description>`
* End Tag: `</opcon:experimental:description>`

Descriptive text that is only included as part of the field description within the experimental CRD.
All text between the tags is included in the experimental CRD, but removed from the standard CRD.

This is only useful if the field is included in the standard CRD, but there's additional meaning in
the experimental CRD when feature gates are enabled.

## Standard Description

* Start Tag: `<opcon:standard:description>`
* End Tag: `</opcon:standard:description>`

Descriptive text that is only included as part of the field description within the standard CRD.
All text between the tags is included in the standard CRD, but removed from the experimental CRD.

This is useful if the field is included in the standard CRD and has differing meaning than when the
field is used in the experimental CRD when feature gates are enabled.


## Exclude from CRD Description

* Start Tag: `<opcon:util:excludeFromCRD>`
* End Tag: `</opcon:util:excludeFromCRD>`

Descriptive text that is excluded from the CRD description. This is similar to the use of `---`, except
the three hypens excludes *all* following text.
