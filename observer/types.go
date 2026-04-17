package observer

// Schema represents inferred schema metadata for a field, parameter, or body.
// Invariant: values are NEVER stored here — only type, format, and structural
// presence. See CLAUDE.md invariant 6.
type Schema struct {
	// Type is one of: string, integer, number, boolean, array, object, binary.
	Type string `json:"type"`

	// Format is one of: uuid, email, date-time, uri, ipv4, ipv6, binary, protobuf.
	Format string `json:"format,omitempty"`

	// Items describes the element schema for array types.
	Items *Schema `json:"items,omitempty"`

	// Properties describes the fields of an object type.
	// Pointer values allow recursive nesting without cycles at the type level.
	Properties map[string]*Schema `json:"properties,omitempty"`

	// Required lists the property names that are required for object types.
	Required []string `json:"required,omitempty"`

	// Nullable indicates the field may be null in addition to its declared type.
	Nullable bool `json:"nullable,omitempty"`

	// Enum lists the observed discrete string values inferred as an enumeration.
	Enum []string `json:"enum,omitempty"`

	// MinLength is the minimum observed string length.
	MinLength int `json:"minLength,omitempty"`

	// MaxLength is the maximum observed string length.
	MaxLength int `json:"maxLength,omitempty"`

	// Minimum is the minimum observed numeric value.
	Minimum *float64 `json:"minimum,omitempty"`

	// Maximum is the maximum observed numeric value.
	Maximum *float64 `json:"maximum,omitempty"`

	// Pattern is a regex pattern inferred from observed string values.
	Pattern string `json:"pattern,omitempty"`

	// Description is a human-readable annotation for the field.
	Description string `json:"description,omitempty"`

	// XMLAttr is true when this field was inferred from an XML attribute
	// (vs. an XML element).
	XMLAttr bool `json:"xml_attr,omitempty"`
}

// CapturedPair holds a single observed request/response pair from the proxy.
// Raw path is stored as-is — normalization happens at read time (CLAUDE.md invariant 7).
// ReqBody and RespBody are pre-capped at 1MB by capture.go before being placed here.
type CapturedPair struct {
	Method          string
	RawPath         string // NOT normalized; patterns computed on read
	ReqContentType  string
	RespContentType string
	StatusCode      int
	LatencyMs       int64
	ReqBody         []byte   // pre-capped at 1MB by capture.go
	RespBody        []byte   // pre-capped at 1MB by capture.go
	ReqHeaders      []string // request header names to track; values never stored
}
