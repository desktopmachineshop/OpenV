package exports

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attributes"
	"github.com/openv/requirements-platform/internal/domain/links"
)

// ReqIF export (issue #224). Produces a ReqIF 1.x document — the OMG
// interchange format DOORS and Polarion read — from a ProjectExport, using
// only encoding/xml (no new dependency). The inverse import is reqif_import.go
// (issue #238).
//
// Fidelity notes:
//   - Attachments are NOT round-tripped in either direction. ReqIF can carry
//     embedded/referenced objects, but per issues #224/#238 attachment payloads
//     are out of scope; only artifact attribute/text content crosses. A ReqIF
//     produced elsewhere that carries external objects imports its text/attribute
//     content and silently drops the attachments.
//   - Body/text is exported as an XHTML-typed value (ATTRIBUTE-VALUE-XHTML,
//     issue #238) so hard line breaks survive: each source newline becomes an
//     <xhtml:br/> inside the THE-VALUE div, and special characters are XML-escaped
//     per line. This replaces the earlier STRING (THE-VALUE attribute) encoding,
//     whose attribute-value normalization collapsed literal newlines to spaces.
//     Title/status/version stay STRING (single-line, no break semantics).
//     Angle brackets, ampersands, quotes and Unicode round-trip exactly.

const (
	// reqIFNamespace is the ReqIF 1.x schema namespace. Consumers key off this
	// to recognise the document as ReqIF.
	reqIFNamespace = "http://www.omg.org/spec/ReqIF/20110401/reqif.xsd"
	// reqIFToolID identifies OpenV as the producing tool in the header.
	reqIFToolID = "OpenV"
	// reqIFStringMaxLength is the declared MAX-LENGTH for the shared STRING
	// datatype. Generous so long bodies are never rejected by strict consumers.
	reqIFStringMaxLength = "1000000"

	// reqIFXHTMLDatatypeID is the shared DATATYPE-DEFINITION-XHTML that the body
	// (text) attribute definition of every SPEC-OBJECT-TYPE points at. Bodies are
	// XHTML-typed so multi-line content keeps its hard line breaks (issue #238).
	reqIFXHTMLDatatypeID = "DT-XHTML"
	// reqIFXHTMLNamespace is the XHTML namespace bound to the xhtml: prefix used
	// inside a THE-VALUE div. Declared inline on the div so the fragment is
	// namespace-well-formed on its own.
	reqIFXHTMLNamespace = "http://www.w3.org/1999/xhtml"

	// Attribute-definition identifier prefixes. Standard fields and org/project
	// attributes are namespaced apart so an org attribute keyed "title" can never
	// collide with the standard Title definition.
	adStdPrefix  = "std"
	adAttrPrefix = "attr"
)

// internalAttributeKeys are attribute-map keys that mirror first-class fields or
// hold engine bookkeeping; they are never exported as ReqIF attributes.
var internalAttributeKeys = map[string]bool{
	"status":          true, // mirrored by the first-class Status field
	"links_snapshot":  true,
	"images_snapshot": true,
	"origin":          true,
}

// --- ReqIF XML document model (hyphenated element names via field tags). ---

type xReqIF struct {
	XMLName     xml.Name     `xml:"REQ-IF"`
	Xmlns       string       `xml:"xmlns,attr"`
	Header      xTheHeader   `xml:"THE-HEADER"`
	CoreContent xCoreContent `xml:"CORE-CONTENT"`
}

type xTheHeader struct {
	ReqIFHeader xReqIFHeader `xml:"REQ-IF-HEADER"`
}

type xReqIFHeader struct {
	Identifier   string `xml:"IDENTIFIER,attr"`
	Comment      string `xml:"COMMENT,omitempty"`
	CreationTime string `xml:"CREATION-TIME"`
	RepositoryID string `xml:"REPOSITORY-ID,omitempty"`
	ReqIFToolID  string `xml:"REQ-IF-TOOL-ID"`
	ReqIFVersion string `xml:"REQ-IF-VERSION"`
	SourceToolID string `xml:"SOURCE-TOOL-ID"`
	Title        string `xml:"TITLE"`
}

type xCoreContent struct {
	ReqIFContent xReqIFContent `xml:"REQ-IF-CONTENT"`
}

type xReqIFContent struct {
	Datatypes      xDatatypes      `xml:"DATATYPES"`
	SpecTypes      xSpecTypes      `xml:"SPEC-TYPES"`
	SpecObjects    xSpecObjects    `xml:"SPEC-OBJECTS"`
	SpecRelations  xSpecRelations  `xml:"SPEC-RELATIONS"`
	Specifications xSpecifications `xml:"SPECIFICATIONS"`
}

type xDatatypes struct {
	Strings []xDatatypeString `xml:"DATATYPE-DEFINITION-STRING"`
	XHTMLs  []xDatatypeXHTML  `xml:"DATATYPE-DEFINITION-XHTML"`
	Enums   []xDatatypeEnum   `xml:"DATATYPE-DEFINITION-ENUMERATION"`
}

type xDatatypeString struct {
	Identifier string `xml:"IDENTIFIER,attr"`
	LongName   string `xml:"LONG-NAME,attr"`
	LastChange string `xml:"LAST-CHANGE,attr"`
	MaxLength  string `xml:"MAX-LENGTH,attr"`
}

// xDatatypeXHTML is the DATATYPE-DEFINITION-XHTML the body attribute references
// so multi-line text keeps its line breaks (issue #238).
type xDatatypeXHTML struct {
	Identifier string `xml:"IDENTIFIER,attr"`
	LongName   string `xml:"LONG-NAME,attr"`
	LastChange string `xml:"LAST-CHANGE,attr"`
}

type xDatatypeEnum struct {
	Identifier      string       `xml:"IDENTIFIER,attr"`
	LongName        string       `xml:"LONG-NAME,attr"`
	LastChange      string       `xml:"LAST-CHANGE,attr"`
	SpecifiedValues []xEnumValue `xml:"SPECIFIED-VALUES>ENUM-VALUE"`
}

type xEnumValue struct {
	Identifier string         `xml:"IDENTIFIER,attr"`
	LongName   string         `xml:"LONG-NAME,attr"`
	LastChange string         `xml:"LAST-CHANGE,attr"`
	Properties xEnumValueProp `xml:"PROPERTIES"`
}

type xEnumValueProp struct {
	Embedded xEmbeddedValue `xml:"EMBEDDED-VALUE"`
}

type xEmbeddedValue struct {
	Key          string `xml:"KEY,attr"`
	OtherContent string `xml:"OTHER-CONTENT,attr"`
}

type xSpecTypes struct {
	SpecObjectTypes    []xSpecObjectType    `xml:"SPEC-OBJECT-TYPE"`
	SpecRelationTypes  []xSpecRelationType  `xml:"SPEC-RELATION-TYPE"`
	SpecificationTypes []xSpecificationType `xml:"SPECIFICATION-TYPE"`
}

type xSpecObjectType struct {
	Identifier  string           `xml:"IDENTIFIER,attr"`
	LongName    string           `xml:"LONG-NAME,attr"`
	LastChange  string           `xml:"LAST-CHANGE,attr"`
	StringAttrs []xAttrDefString `xml:"SPEC-ATTRIBUTES>ATTRIBUTE-DEFINITION-STRING"`
	XHTMLAttrs  []xAttrDefXHTML  `xml:"SPEC-ATTRIBUTES>ATTRIBUTE-DEFINITION-XHTML"`
	EnumAttrs   []xAttrDefEnum   `xml:"SPEC-ATTRIBUTES>ATTRIBUTE-DEFINITION-ENUMERATION"`
}

type xAttrDefString struct {
	Identifier string `xml:"IDENTIFIER,attr"`
	LongName   string `xml:"LONG-NAME,attr"`
	LastChange string `xml:"LAST-CHANGE,attr"`
	TypeRef    string `xml:"TYPE>DATATYPE-DEFINITION-STRING-REF"`
}

// xAttrDefXHTML defines the body (text) attribute as XHTML-typed on a
// SPEC-OBJECT-TYPE (issue #238).
type xAttrDefXHTML struct {
	Identifier string `xml:"IDENTIFIER,attr"`
	LongName   string `xml:"LONG-NAME,attr"`
	LastChange string `xml:"LAST-CHANGE,attr"`
	TypeRef    string `xml:"TYPE>DATATYPE-DEFINITION-XHTML-REF"`
}

type xAttrDefEnum struct {
	Identifier  string `xml:"IDENTIFIER,attr"`
	LongName    string `xml:"LONG-NAME,attr"`
	LastChange  string `xml:"LAST-CHANGE,attr"`
	MultiValued string `xml:"MULTI-VALUED,attr"`
	TypeRef     string `xml:"TYPE>DATATYPE-DEFINITION-ENUMERATION-REF"`
}

type xSpecRelationType struct {
	Identifier string `xml:"IDENTIFIER,attr"`
	LongName   string `xml:"LONG-NAME,attr"`
	LastChange string `xml:"LAST-CHANGE,attr"`
}

type xSpecificationType struct {
	Identifier string `xml:"IDENTIFIER,attr"`
	LongName   string `xml:"LONG-NAME,attr"`
	LastChange string `xml:"LAST-CHANGE,attr"`
}

type xSpecObjects struct {
	SpecObjects []xSpecObject `xml:"SPEC-OBJECT"`
}

type xSpecObject struct {
	Identifier   string           `xml:"IDENTIFIER,attr"`
	LastChange   string           `xml:"LAST-CHANGE,attr"`
	StringValues []xAttrValString `xml:"VALUES>ATTRIBUTE-VALUE-STRING"`
	XHTMLValues  []xAttrValXHTML  `xml:"VALUES>ATTRIBUTE-VALUE-XHTML"`
	EnumValues   []xAttrValEnum   `xml:"VALUES>ATTRIBUTE-VALUE-ENUMERATION"`
	TypeRef      string           `xml:"TYPE>SPEC-OBJECT-TYPE-REF"`
}

type xAttrValString struct {
	TheValue string `xml:"THE-VALUE,attr"`
	DefRef   string `xml:"DEFINITION>ATTRIBUTE-DEFINITION-STRING-REF"`
}

// xAttrValXHTML is a body value: THE-VALUE holds inline XHTML content (a div of
// escaped text with <xhtml:br/> per newline). Inner is captured verbatim on the
// way out (raw pre-built markup) and on the way back in (for xhtmlToPlainText).
type xAttrValXHTML struct {
	TheValue xXHTMLContent `xml:"THE-VALUE"`
	DefRef   string        `xml:"DEFINITION>ATTRIBUTE-DEFINITION-XHTML-REF"`
}

type xXHTMLContent struct {
	Inner string `xml:",innerxml"`
}

type xAttrValEnum struct {
	DefRef    string   `xml:"DEFINITION>ATTRIBUTE-DEFINITION-ENUMERATION-REF"`
	ValueRefs []string `xml:"VALUES>ENUM-VALUE-REF"`
}

type xSpecRelations struct {
	SpecRelations []xSpecRelation `xml:"SPEC-RELATION"`
}

type xSpecRelation struct {
	Identifier string `xml:"IDENTIFIER,attr"`
	LastChange string `xml:"LAST-CHANGE,attr"`
	TypeRef    string `xml:"TYPE>SPEC-RELATION-TYPE-REF"`
	SourceRef  string `xml:"SOURCE>SPEC-OBJECT-REF"`
	TargetRef  string `xml:"TARGET>SPEC-OBJECT-REF"`
}

type xSpecifications struct {
	Specifications []xSpecification `xml:"SPECIFICATION"`
}

type xSpecification struct {
	Identifier string              `xml:"IDENTIFIER,attr"`
	LongName   string              `xml:"LONG-NAME,attr"`
	LastChange string              `xml:"LAST-CHANGE,attr"`
	TypeRef    string              `xml:"TYPE>SPECIFICATION-TYPE-REF"`
	Children   *xHierarchyChildren `xml:"CHILDREN,omitempty"`
}

type xSpecHierarchy struct {
	Identifier string              `xml:"IDENTIFIER,attr"`
	LastChange string              `xml:"LAST-CHANGE,attr"`
	ObjectRef  string              `xml:"OBJECT>SPEC-OBJECT-REF"`
	Children   *xHierarchyChildren `xml:"CHILDREN,omitempty"`
}

// xHierarchyChildren wraps a CHILDREN element. A nil pointer omits CHILDREN
// entirely (leaf nodes), avoiding an empty <CHILDREN/> which ReqIF's schema —
// CHILDREN requires at least one SPEC-HIERARCHY — would reject.
type xHierarchyChildren struct {
	Items []xSpecHierarchy `xml:"SPEC-HIERARCHY"`
}

// --- builders ---

// typeAttrInfo records, per artifact type, the attribute definitions a
// SPEC-OBJECT of that type must emit values against, in emission order.
type typeAttrInfo struct {
	// stringDefs pairs an ATTRIBUTE-DEFINITION-STRING identifier with the
	// logical key used to pull the value from an artifact. Standard fields use
	// the pseudo-keys "identifier"/"title"/"text"/"status"/"version"; org and
	// discovered attributes use their real attribute-map key.
	stringDefs []attrDefRef
	// enumDefs pairs an ATTRIBUTE-DEFINITION-ENUMERATION identifier with the
	// attribute-map key and the definition's enum value id lookup.
	enumDefs []enumDefRef
	// textDefID is the ATTRIBUTE-DEFINITION-XHTML identifier the body value is
	// emitted against for this type (issue #238).
	textDefID string
}

type attrDefRef struct {
	id  string
	key string
	std bool // true for the standard title/text/status/version/identifier fields
}

type enumDefRef struct {
	id  string
	key string
}

// exportReqIF renders a ProjectExport as a ReqIF 1.x XML document.
func (s *DefaultService) exportReqIF(data *ProjectExport) ([]byte, string, error) {
	doc, err := buildReqIF(data)
	if err != nil {
		return nil, "", fmt.Errorf("failed to build ReqIF: %w", err)
	}
	return doc, exportFilename(data.ProjectName, "reqif"), nil
}

// buildReqIF assembles the ReqIF document bytes from a ProjectExport. Exported
// identifiers are stable (derived from artifact/link ids) so re-exporting an
// unchanged project yields a diff-stable document, and all text is escaped by
// encoding/xml.
func buildReqIF(data *ProjectExport) ([]byte, error) {
	ts := data.ExportedAt
	if ts.IsZero() {
		ts = time.Now()
	}
	lastChange := ts.UTC().Format(time.RFC3339)

	// 1. Enum datatypes, one per enum attribute definition (dedup by key,
	//    first definition wins). enumValueID[key][value] -> ENUM-VALUE ref.
	var enumDatatypes []xDatatypeEnum
	enumDatatypeID := map[string]string{}         // key -> DATATYPE-DEFINITION-ENUMERATION id
	enumValueID := map[string]map[string]string{} // key -> value -> ENUM-VALUE id
	for _, def := range data.AttributeDefs {
		if def == nil || def.DataType != attributes.DataTypeEnum {
			continue
		}
		if _, seen := enumDatatypeID[def.Key]; seen {
			continue
		}
		dtID := "DT-ENUM-" + reqifSanitizeID(def.Key)
		enumDatatypeID[def.Key] = dtID
		values := map[string]string{}
		var enumValues []xEnumValue
		for i, v := range def.EnumValues {
			evID := fmt.Sprintf("EV-%s-%d", reqifSanitizeID(def.Key), i)
			values[v] = evID
			enumValues = append(enumValues, xEnumValue{
				Identifier: evID,
				LongName:   v,
				LastChange: lastChange,
				Properties: xEnumValueProp{Embedded: xEmbeddedValue{
					Key:          strconv.Itoa(i),
					OtherContent: v,
				}},
			})
		}
		enumValueID[def.Key] = values
		enumDatatypes = append(enumDatatypes, xDatatypeEnum{
			Identifier:      dtID,
			LongName:        def.Label,
			LastChange:      lastChange,
			SpecifiedValues: enumValues,
		})
	}

	// 2. The set of artifact types to model: the catalog plus any type actually
	//    present on an artifact (so every SPEC-OBJECT references a real type).
	typeLabel := map[string]string{}
	var typeOrder []string
	addType := func(value, label string) {
		if _, ok := typeLabel[value]; ok {
			return
		}
		typeLabel[value] = label
		typeOrder = append(typeOrder, value)
	}
	for _, td := range artifacts.TypeCatalog() {
		addType(td.Value, td.Label)
	}
	for _, a := range data.Artifacts {
		if a != nil && a.Type != "" {
			addType(a.Type, a.Type)
		}
	}

	// 3. Discovered extra attribute keys per type: keys present on artifacts
	//    that are neither internal nor already covered by a definition, and that
	//    carry at least one scalar value. Kept sorted for a stable document.
	defKeysForType := func(t string) map[string]bool {
		out := map[string]bool{}
		for _, def := range data.AttributeDefs {
			if def == nil {
				continue
			}
			if def.AppliesToType == "" || def.AppliesToType == t {
				out[def.Key] = true
			}
		}
		return out
	}
	discovered := map[string]map[string]bool{}
	for _, a := range data.Artifacts {
		if a == nil {
			continue
		}
		covered := defKeysForType(a.Type)
		for k, v := range a.Attributes {
			if internalAttributeKeys[k] || covered[k] {
				continue
			}
			if _, ok := attrValueToString(v); !ok {
				continue
			}
			if discovered[a.Type] == nil {
				discovered[a.Type] = map[string]bool{}
			}
			discovered[a.Type][k] = true
		}
	}

	// 4. SPEC-OBJECT-TYPEs with their attribute definitions, remembering how
	//    each type's SPEC-OBJECTs should emit values.
	// Standard string fields. "text" (the body) is handled separately as an
	// XHTML-typed attribute so multi-line content keeps its line breaks.
	stdFields := []struct{ key, label string }{
		{"identifier", "Identifier"},
		{"title", "Title"},
		{"status", "Status"},
		{"version", "Version"},
	}
	var specObjectTypes []xSpecObjectType
	attrInfoByType := map[string]typeAttrInfo{}
	for _, t := range typeOrder {
		var info typeAttrInfo
		var stringAttrDefs []xAttrDefString
		var xhtmlAttrDefs []xAttrDefXHTML
		var enumAttrDefs []xAttrDefEnum

		// Standard string fields.
		for _, f := range stdFields {
			id := adID(t, adStdPrefix, f.key)
			stringAttrDefs = append(stringAttrDefs, xAttrDefString{
				Identifier: id,
				LongName:   f.label,
				LastChange: lastChange,
				TypeRef:    "DT-STRING",
			})
			info.stringDefs = append(info.stringDefs, attrDefRef{id: id, key: f.key, std: true})
		}

		// Body/text field: XHTML-typed (issue #238).
		info.textDefID = adID(t, adStdPrefix, "text")
		xhtmlAttrDefs = append(xhtmlAttrDefs, xAttrDefXHTML{
			Identifier: info.textDefID,
			LongName:   "Text",
			LastChange: lastChange,
			TypeRef:    reqIFXHTMLDatatypeID,
		})

		// Org/project attribute definitions applicable to this type.
		for _, def := range data.AttributeDefs {
			if def == nil {
				continue
			}
			if def.AppliesToType != "" && def.AppliesToType != t {
				continue
			}
			id := adID(t, adAttrPrefix, def.Key)
			if def.DataType == attributes.DataTypeEnum {
				enumAttrDefs = append(enumAttrDefs, xAttrDefEnum{
					Identifier:  id,
					LongName:    def.Label,
					LastChange:  lastChange,
					MultiValued: "false",
					TypeRef:     enumDatatypeID[def.Key],
				})
				info.enumDefs = append(info.enumDefs, enumDefRef{id: id, key: def.Key})
			} else {
				stringAttrDefs = append(stringAttrDefs, xAttrDefString{
					Identifier: id,
					LongName:   def.Label,
					LastChange: lastChange,
					TypeRef:    "DT-STRING",
				})
				info.stringDefs = append(info.stringDefs, attrDefRef{id: id, key: def.Key})
			}
		}

		// Discovered free-form attributes (always STRING).
		for _, k := range sortedKeys(discovered[t]) {
			id := adID(t, adAttrPrefix, k)
			stringAttrDefs = append(stringAttrDefs, xAttrDefString{
				Identifier: id,
				LongName:   k,
				LastChange: lastChange,
				TypeRef:    "DT-STRING",
			})
			info.stringDefs = append(info.stringDefs, attrDefRef{id: id, key: k})
		}

		specObjectTypes = append(specObjectTypes, xSpecObjectType{
			Identifier:  "SOT-" + reqifSanitizeID(t),
			LongName:    typeLabel[t],
			LastChange:  lastChange,
			StringAttrs: stringAttrDefs,
			XHTMLAttrs:  xhtmlAttrDefs,
			EnumAttrs:   enumAttrDefs,
		})
		attrInfoByType[t] = info
	}

	// 5. SPEC-RELATION-TYPEs, one per link-type rule.
	var specRelationTypes []xSpecRelationType
	for _, rule := range links.GetLinkTypeRules() {
		specRelationTypes = append(specRelationTypes, xSpecRelationType{
			Identifier: "SRT-" + reqifSanitizeID(rule.Type),
			LongName:   rule.Label,
			LastChange: lastChange,
		})
	}
	// A link may carry a type outside the rule table; ensure a SPEC-RELATION-TYPE
	// exists for every type used so no SPEC-RELATION dangles.
	seenRelType := map[string]bool{}
	for _, rt := range specRelationTypes {
		seenRelType[rt.Identifier] = true
	}
	for _, l := range data.Links {
		if l == nil {
			continue
		}
		id := "SRT-" + reqifSanitizeID(l.Type)
		if !seenRelType[id] {
			seenRelType[id] = true
			specRelationTypes = append(specRelationTypes, xSpecRelationType{
				Identifier: id,
				LongName:   l.Type,
				LastChange: lastChange,
			})
		}
	}

	// 6. SPEC-OBJECTs, one per artifact.
	objectExists := map[string]bool{}
	var specObjects []xSpecObject
	for _, a := range data.Artifacts {
		if a == nil {
			continue
		}
		info := attrInfoByType[a.Type]
		var stringVals []xAttrValString
		for _, ref := range info.stringDefs {
			val, ok := valueForRef(a, ref)
			if !ok {
				continue
			}
			stringVals = append(stringVals, xAttrValString{TheValue: val, DefRef: ref.id})
		}
		// Body as an XHTML value (issue #238). Only emitted when non-empty so an
		// empty body leaves no value, and import reads its absence back as "".
		var xhtmlVals []xAttrValXHTML
		if a.Body != "" && info.textDefID != "" {
			xhtmlVals = append(xhtmlVals, xAttrValXHTML{
				TheValue: xXHTMLContent{Inner: bodyToXHTML(a.Body)},
				DefRef:   info.textDefID,
			})
		}
		var enumVals []xAttrValEnum
		for _, ref := range info.enumDefs {
			raw, ok := a.Attributes[ref.key]
			if !ok {
				continue
			}
			sv, ok := raw.(string)
			if !ok || sv == "" {
				continue
			}
			evID, ok := enumValueID[ref.key][sv]
			if !ok {
				continue // value outside the definition's allowed set
			}
			enumVals = append(enumVals, xAttrValEnum{DefRef: ref.id, ValueRefs: []string{evID}})
		}
		specObjects = append(specObjects, xSpecObject{
			Identifier:   a.ID,
			LastChange:   lastChange,
			StringValues: stringVals,
			XHTMLValues:  xhtmlVals,
			EnumValues:   enumVals,
			TypeRef:      "SOT-" + reqifSanitizeID(a.Type),
		})
		objectExists[a.ID] = true
	}

	// 7. SPEC-RELATIONs, one per link whose endpoints are both exported.
	var specRelations []xSpecRelation
	for _, l := range data.Links {
		if l == nil {
			continue
		}
		if !objectExists[l.FromID] || !objectExists[l.ToID] {
			continue // dangling endpoint; skip to keep every ref resolvable
		}
		specRelations = append(specRelations, xSpecRelation{
			Identifier: l.ID,
			LastChange: lastChange,
			TypeRef:    "SRT-" + reqifSanitizeID(l.Type),
			SourceRef:  l.FromID,
			TargetRef:  l.ToID,
		})
	}

	// 8. SPECIFICATION: the parent_id outline as a SPEC-HIERARCHY tree so
	//    DOORS/Polarion render the module structure.
	var specChildren *xHierarchyChildren
	if roots := buildHierarchy(data.Artifacts, objectExists, lastChange); len(roots) > 0 {
		specChildren = &xHierarchyChildren{Items: roots}
	}

	doc := xReqIF{
		Xmlns: reqIFNamespace,
		Header: xTheHeader{ReqIFHeader: xReqIFHeader{
			Identifier:   "HDR-" + nonEmpty(data.ProjectID, "project"),
			Comment:      "Exported from OpenV",
			CreationTime: lastChange,
			RepositoryID: data.ProjectID,
			ReqIFToolID:  reqIFToolID,
			ReqIFVersion: "1.0",
			SourceToolID: reqIFToolID,
			Title:        data.ProjectName,
		}},
		CoreContent: xCoreContent{ReqIFContent: xReqIFContent{
			Datatypes: xDatatypes{
				Strings: []xDatatypeString{{
					Identifier: "DT-STRING",
					LongName:   "String",
					LastChange: lastChange,
					MaxLength:  reqIFStringMaxLength,
				}},
				XHTMLs: []xDatatypeXHTML{{
					Identifier: reqIFXHTMLDatatypeID,
					LongName:   "XHTML",
					LastChange: lastChange,
				}},
				Enums: enumDatatypes,
			},
			SpecTypes: xSpecTypes{
				SpecObjectTypes:   specObjectTypes,
				SpecRelationTypes: specRelationTypes,
				SpecificationTypes: []xSpecificationType{{
					Identifier: "SPT-outline",
					LongName:   "OpenV Outline",
					LastChange: lastChange,
				}},
			},
			SpecObjects:   xSpecObjects{SpecObjects: specObjects},
			SpecRelations: xSpecRelations{SpecRelations: specRelations},
			Specifications: xSpecifications{Specifications: []xSpecification{{
				Identifier: "SPEC-" + nonEmpty(data.ProjectID, "project"),
				LongName:   data.ProjectName,
				LastChange: lastChange,
				TypeRef:    "SPT-outline",
				Children:   specChildren,
			}}},
		}},
	}

	var buf bytes.Buffer
	buf.WriteString(xml.Header) // <?xml version="1.0" encoding="UTF-8"?>\n
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	if err := enc.Flush(); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

// buildHierarchy turns the artifact parent_id relation into a SPEC-HIERARCHY
// forest. Roots are artifacts with no parent (or a parent outside the export).
// Children are ordered by SortOrder then original order, and a visited guard
// keeps a malformed cyclic parent chain from recursing forever.
func buildHierarchy(artifactList []*artifacts.Artifact, objectExists map[string]bool, lastChange string) []xSpecHierarchy {
	type node struct {
		a   *artifacts.Artifact
		pos int
	}
	childrenOf := map[string][]node{}
	var roots []node
	for i, a := range artifactList {
		if a == nil {
			continue
		}
		n := node{a: a, pos: i}
		if a.ParentID != nil && *a.ParentID != "" && objectExists[*a.ParentID] {
			childrenOf[*a.ParentID] = append(childrenOf[*a.ParentID], n)
		} else {
			roots = append(roots, n)
		}
	}
	sortNodes := func(ns []node) {
		sort.SliceStable(ns, func(i, j int) bool {
			if ns[i].a.SortOrder != ns[j].a.SortOrder {
				return ns[i].a.SortOrder < ns[j].a.SortOrder
			}
			return ns[i].pos < ns[j].pos
		})
	}
	visited := map[string]bool{}
	var build func(ns []node) []xSpecHierarchy
	build = func(ns []node) []xSpecHierarchy {
		sortNodes(ns)
		out := make([]xSpecHierarchy, 0, len(ns))
		for _, n := range ns {
			if visited[n.a.ID] {
				continue
			}
			visited[n.a.ID] = true
			sh := xSpecHierarchy{
				Identifier: "SH-" + n.a.ID,
				LastChange: lastChange,
				ObjectRef:  n.a.ID,
			}
			if kids := childrenOf[n.a.ID]; len(kids) > 0 {
				sh.Children = &xHierarchyChildren{Items: build(kids)}
			}
			out = append(out, sh)
		}
		return out
	}
	return build(roots)
}

// valueForRef resolves the string value a SPEC-OBJECT should emit for one
// attribute-definition reference. Standard fields read first-class artifact
// fields; other refs read the attributes map. Absent/non-scalar attribute
// values yield ok=false so no empty ATTRIBUTE-VALUE is written for them.
func valueForRef(a *artifacts.Artifact, ref attrDefRef) (string, bool) {
	if ref.std {
		switch ref.key {
		case "identifier":
			return a.ID, true
		case "title":
			return a.Title, true
		case "text":
			return a.Body, true
		case "status":
			return artifactStatus(a), true
		case "version":
			return strconv.Itoa(a.Version), true
		}
		return "", false
	}
	raw, ok := a.Attributes[ref.key]
	if !ok {
		return "", false
	}
	return attrValueToString(raw)
}

// artifactStatus prefers the first-class Status column, falling back to the
// legacy Attributes["status"] mirror for pre-column exports.
func artifactStatus(a *artifacts.Artifact) string {
	if a.Status != "" {
		return a.Status
	}
	if a.Attributes != nil {
		if v, ok := a.Attributes["status"].(string); ok {
			return v
		}
	}
	return ""
}

// attrValueToString renders a scalar attribute value as a ReqIF STRING. Complex
// values (maps, slices such as links_snapshot) return ok=false and are skipped.
func attrValueToString(v interface{}) (string, bool) {
	switch t := v.(type) {
	case nil:
		return "", false
	case string:
		return t, true
	case bool:
		return strconv.FormatBool(t), true
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), true
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 64), true
	case int:
		return strconv.Itoa(t), true
	case int64:
		return strconv.FormatInt(t, 10), true
	default:
		return "", false
	}
}

// bodyToXHTML renders an artifact body as the inline XHTML fragment that goes
// inside a THE-VALUE element (issue #238). Each source newline becomes an
// <xhtml:br/> so hard line breaks survive the round trip; every text run is
// XML-escaped so angle brackets, ampersands and quotes stay literal. The xhtml
// namespace is declared inline so the fragment is self-contained. xhtmlToPlainText
// in reqif_import.go is the inverse.
func bodyToXHTML(body string) string {
	normalized := strings.ReplaceAll(body, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	lines := strings.Split(normalized, "\n")
	var b strings.Builder
	b.WriteString(`<xhtml:div xmlns:xhtml="`)
	b.WriteString(reqIFXHTMLNamespace)
	b.WriteString(`">`)
	for i, line := range lines {
		if i > 0 {
			b.WriteString("<xhtml:br/>")
		}
		b.WriteString(escapeXHTMLText(line))
	}
	b.WriteString("</xhtml:div>")
	return b.String()
}

// escapeXHTMLText XML-escapes one line of body text (no newlines: the caller
// splits on them first).
func escapeXHTMLText(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

// adID builds a stable, collision-free ATTRIBUTE-DEFINITION identifier scoped to
// an artifact type and namespace ("std" for first-class fields, "attr" for
// org/discovered attributes).
func adID(artifactType, namespace, key string) string {
	return "AD-" + reqifSanitizeID(artifactType) + "-" + namespace + "-" + reqifSanitizeID(key)
}

// reqifSanitizeID reduces an arbitrary string to characters safe inside a ReqIF
// IDENTIFIER, keeping the input readable. It is only used for constructing
// derived type/attribute ids, never for the artifact/link ids themselves (those
// are already opaque and stable).
func reqifSanitizeID(s string) string {
	if s == "" {
		return "x"
	}
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}

// sortedKeys returns a set's keys in sorted order for deterministic output.
func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// nonEmpty returns v, or fallback when v is empty.
func nonEmpty(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
