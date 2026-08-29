package exports

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"

	"github.com/openv/requirements-platform/internal/domain/artifacts"
	"github.com/openv/requirements-platform/internal/domain/attributes"
	"github.com/openv/requirements-platform/internal/domain/links"
)

// ReqIF import (issue #238). The inverse of buildReqIF: it parses a ReqIF 1.x
// document into a ProjectExport so the SAME import machinery the JSON importer
// uses (importArtifactsAndLinks — id remapping, parent reconstruction, link
// creation, version=1) can apply it. It reads best-effort ReqIF from other
// tools too, but is tuned to round-trip documents this service produced.
//
// Mapping (exporter-inverse):
//   - SPEC-OBJECT            -> artifact. AD-<type>-std-{identifier,title,text,
//                              status,version} carry the first-class fields; the
//                              XHTML-typed body becomes plain text (line breaks
//                              recovered from <xhtml:br/>); other attribute
//                              values become Attributes entries.
//   - ATTRIBUTE-VALUE-ENUM   -> the enum value's text (resolved via the enum
//                              datatype), validated against the datatype's
//                              declared values (attributes.ValidateAttributes).
//   - SPEC-RELATION          -> link (SRT-<type> -> link type).
//   - SPEC-HIERARCHY         -> parent_id + sort order.
//
// Out of scope (both directions): attachments / external objects are dropped.

// parseReqIF decodes ReqIF bytes into a ProjectExport. A document that is not
// well-formed XML, or whose root is not REQ-IF, yields an error the handler
// maps to 400. An enum attribute whose value is not among its datatype's
// declared values is also rejected.
func parseReqIF(data []byte) (*ProjectExport, error) {
	var doc xReqIF
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("malformed ReqIF: %w", err)
	}
	content := doc.CoreContent.ReqIFContent

	// 1. Enum value ids -> their text, and each enum datatype -> its declared
	//    value set (for validating incoming enum attributes).
	enumValueText := map[string]string{}        // ENUM-VALUE id -> value text
	enumDatatypeValues := map[string][]string{} // datatype id -> allowed values
	for _, dt := range content.Datatypes.Enums {
		vals := make([]string, 0, len(dt.SpecifiedValues))
		for _, ev := range dt.SpecifiedValues {
			text := ev.LongName
			if text == "" {
				text = ev.Properties.Embedded.OtherContent
			}
			enumValueText[ev.Identifier] = text
			vals = append(vals, text)
		}
		enumDatatypeValues[dt.Identifier] = vals
	}

	// 2. Per SPEC-OBJECT-TYPE, resolve each attribute-definition id to what it
	//    means (a standard field or an attribute key) and recover the type value.
	type stringAttrMeta struct {
		std bool
		key string
	}
	type enumAttrMeta struct {
		key        string
		datatypeID string
	}
	// typeByRef maps a SPEC-OBJECT-TYPE-REF (SOT id) to the artifact type value.
	typeByRef := map[string]string{}
	stringMetaByType := map[string]map[string]stringAttrMeta{} // sotID -> defID -> meta
	xhtmlMetaByType := map[string]map[string]stringAttrMeta{}
	enumMetaByType := map[string]map[string]enumAttrMeta{}
	// enumDefs collects reconstructed definitions for validation.
	var enumDefs []*attributes.Definition
	seenEnumDef := map[string]bool{}

	for _, sot := range content.SpecTypes.SpecObjectTypes {
		typeValue := strings.TrimPrefix(sot.Identifier, "SOT-")
		typeByRef[sot.Identifier] = typeValue

		strMeta := map[string]stringAttrMeta{}
		for _, ad := range sot.StringAttrs {
			strMeta[ad.Identifier] = classifyDef(ad.Identifier, sot.Identifier, ad.LongName)
		}
		stringMetaByType[sot.Identifier] = strMeta

		xhMeta := map[string]stringAttrMeta{}
		for _, ad := range sot.XHTMLAttrs {
			xhMeta[ad.Identifier] = classifyDef(ad.Identifier, sot.Identifier, ad.LongName)
		}
		xhtmlMetaByType[sot.Identifier] = xhMeta

		enMeta := map[string]enumAttrMeta{}
		for _, ad := range sot.EnumAttrs {
			meta := classifyDef(ad.Identifier, sot.Identifier, ad.LongName)
			enMeta[ad.Identifier] = enumAttrMeta{key: meta.key, datatypeID: ad.TypeRef}
			// Reconstruct a definition for enum validation, keyed by
			// (type, key) so each is registered once.
			dedupKey := typeValue + "\x00" + meta.key
			if !seenEnumDef[dedupKey] {
				seenEnumDef[dedupKey] = true
				enumDefs = append(enumDefs, &attributes.Definition{
					Key:           meta.key,
					Label:         ad.LongName,
					DataType:      attributes.DataTypeEnum,
					EnumValues:    enumDatatypeValues[ad.TypeRef],
					AppliesToType: typeValue,
				})
			}
		}
		enumMetaByType[sot.Identifier] = enMeta
	}

	// 3. SPEC-OBJECTs -> artifacts.
	artifactList := make([]*artifacts.Artifact, 0, len(content.SpecObjects.SpecObjects))
	for _, so := range content.SpecObjects.SpecObjects {
		typeValue := typeByRef[so.TypeRef]
		if typeValue == "" {
			typeValue = strings.TrimPrefix(so.TypeRef, "SOT-")
		}
		a := &artifacts.Artifact{
			ID:         so.Identifier,
			Type:       typeValue,
			Attributes: map[string]interface{}{},
		}

		strMeta := stringMetaByType[so.TypeRef]
		for _, v := range so.StringValues {
			meta, ok := strMeta[v.DefRef]
			if !ok {
				meta = classifyDef(v.DefRef, so.TypeRef, "")
			}
			applyStdOrAttr(a, meta.std, meta.key, v.TheValue)
		}

		xhMeta := xhtmlMetaByType[so.TypeRef]
		for _, v := range so.XHTMLValues {
			meta, ok := xhMeta[v.DefRef]
			if !ok {
				meta = classifyDef(v.DefRef, so.TypeRef, "")
			}
			applyStdOrAttr(a, meta.std, meta.key, xhtmlToPlainText(v.TheValue.Inner))
		}

		enMeta := enumMetaByType[so.TypeRef]
		for _, v := range so.EnumValues {
			meta, ok := enMeta[v.DefRef]
			if !ok {
				continue
			}
			var vals []string
			for _, ref := range v.ValueRefs {
				if text, ok := enumValueText[ref]; ok {
					vals = append(vals, text)
				}
			}
			if len(vals) == 0 {
				continue
			}
			a.Attributes[meta.key] = strings.Join(vals, ", ")
		}

		// Preserve the exported ID for parent/link remapping. The std
		// "identifier" value already equals so.Identifier; keep so.Identifier
		// authoritative and never surface it as an attribute.
		delete(a.Attributes, "identifier")

		if len(a.Attributes) == 0 {
			a.Attributes = nil
		}
		artifactList = append(artifactList, a)
	}

	// 4. Validate enum attributes against the datatypes they were declared with.
	//    A value outside its datatype (e.g. a cross-datatype ENUM-VALUE-REF) is a
	//    malformed document, not a silently-dropped attribute.
	if len(enumDefs) > 0 {
		for _, a := range artifactList {
			if a.Attributes == nil {
				continue
			}
			if err := attributes.ValidateAttributes(enumDefs, a.Type, a.Attributes, false); err != nil {
				return nil, fmt.Errorf("ReqIF enum attribute on %q: %w", a.ID, err)
			}
		}
	}

	// 5. SPEC-RELATIONs -> links.
	linkList := make([]*links.Link, 0, len(content.SpecRelations.SpecRelations))
	for _, rel := range content.SpecRelations.SpecRelations {
		linkList = append(linkList, &links.Link{
			ID:     rel.Identifier,
			FromID: rel.SourceRef,
			ToID:   rel.TargetRef,
			Type:   strings.TrimPrefix(rel.TypeRef, "SRT-"),
		})
	}

	// 6. SPEC-HIERARCHY -> parent_id + sort order.
	parentOf, sortOf := hierarchyIndex(content.Specifications.Specifications)
	for _, a := range artifactList {
		if p, ok := parentOf[a.ID]; ok && p != "" {
			parent := p
			a.ParentID = &parent
		}
		if order, ok := sortOf[a.ID]; ok {
			a.SortOrder = order
		}
	}

	projectName := strings.TrimSpace(doc.Header.ReqIFHeader.Title)
	if projectName == "" {
		for _, spec := range content.Specifications.Specifications {
			if strings.TrimSpace(spec.LongName) != "" {
				projectName = strings.TrimSpace(spec.LongName)
				break
			}
		}
	}
	if projectName == "" {
		projectName = "Imported ReqIF Project"
	}

	return &ProjectExport{
		Version:     "1.0",
		ProjectID:   doc.Header.ReqIFHeader.RepositoryID,
		ProjectName: projectName,
		Artifacts:   artifactList,
		Links:       linkList,
	}, nil
}

// classifyDef recovers what an attribute-definition id means. It first inverts
// the exporter's own id scheme (AD-<sanitizedType>-std-<key> /
// AD-<sanitizedType>-attr-<key>, where the type is the SOT id minus its "SOT-"
// prefix), then falls back to the definition's LONG-NAME for third-party ReqIF.
func classifyDef(defID, sotID, longName string) struct {
	std bool
	key string
} {
	type meta = struct {
		std bool
		key string
	}
	sanType := strings.TrimPrefix(sotID, "SOT-")
	if key, ok := strings.CutPrefix(defID, "AD-"+sanType+"-std-"); ok {
		return meta{std: true, key: key}
	}
	if key, ok := strings.CutPrefix(defID, "AD-"+sanType+"-attr-"); ok {
		return meta{std: false, key: key}
	}
	ln := strings.TrimSpace(longName)
	switch strings.ToLower(ln) {
	case "identifier", "title", "text", "status", "version":
		return meta{std: true, key: strings.ToLower(ln)}
	}
	return meta{std: false, key: ln}
}

// applyStdOrAttr writes one resolved value either onto a first-class field
// (std) or into the attributes map.
func applyStdOrAttr(a *artifacts.Artifact, std bool, key, value string) {
	if std {
		switch key {
		case "identifier":
			// Kept only as so.Identifier; ignore here (removed from attrs later).
		case "title":
			a.Title = value
		case "text":
			a.Body = value
		case "status":
			if value != "" {
				a.Status = value
				// NewArtifact seeds the status column from Attributes["status"]
				// on import, so carry it there for the shared create path.
				if a.Attributes == nil {
					a.Attributes = map[string]interface{}{}
				}
				a.Attributes["status"] = value
			}
		case "version":
			if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
				a.Version = n
			}
		}
		return
	}
	if key == "" {
		return
	}
	if a.Attributes == nil {
		a.Attributes = map[string]interface{}{}
	}
	a.Attributes[key] = value
}

// hierarchyIndex walks the SPEC-HIERARCHY forest of every SPECIFICATION and
// returns, per artifact (old) id, its parent's id and its 1-based position
// among its siblings.
func hierarchyIndex(specs []xSpecification) (parentOf map[string]string, sortOf map[string]int) {
	parentOf = map[string]string{}
	sortOf = map[string]int{}
	var walk func(items []xSpecHierarchy, parent string)
	walk = func(items []xSpecHierarchy, parent string) {
		for i, sh := range items {
			if sh.ObjectRef != "" {
				if parent != "" {
					parentOf[sh.ObjectRef] = parent
				}
				sortOf[sh.ObjectRef] = i + 1
			}
			if sh.Children != nil {
				walk(sh.Children.Items, sh.ObjectRef)
			}
		}
	}
	for _, spec := range specs {
		if spec.Children != nil {
			walk(spec.Children.Items, "")
		}
	}
	return parentOf, sortOf
}

// xhtmlToPlainText recovers plain text (with hard line breaks) from the inline
// XHTML captured inside a THE-VALUE element. It is the inverse of bodyToXHTML:
// <br/> -> newline, block ends (</p>) -> newline, and the XML decoder unescapes
// entities and drops the wrapping tags for free. Malformed fragments degrade to
// whatever text was decoded before the error.
func xhtmlToPlainText(inner string) string {
	if strings.TrimSpace(inner) == "" {
		return ""
	}
	dec := xml.NewDecoder(strings.NewReader(inner))
	var b strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.CharData:
			b.Write(t)
		case xml.StartElement:
			if strings.EqualFold(t.Name.Local, "br") {
				b.WriteByte('\n')
			}
		case xml.EndElement:
			if strings.EqualFold(t.Name.Local, "p") {
				b.WriteByte('\n')
			}
		}
	}
	return b.String()
}
