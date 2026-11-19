// The MCP tools over the projects an agent can see. Tools, in tools.go,
// concatenates each area's constructor in the table's order.

package mcp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// projectTools returns the project tools, in Tools() order.
func projectTools() []Tool {
	return []Tool{
		{
			Name:        "list_projects",
			ReadOnly:    true,
			Description: "List all projects visible to this agent.",
			InputSchema: schema(nil, map[string]interface{}{}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("GET", "/api/v1/projects", nil, nil)
				return out, err
			},
		},
		{
			Name: "create_project",
			Description: "Create a project in the workspace. Give parent_project_id to file it under another project of the same workspace, so its requirements can refine that project's and its verification rolls up there. " +
				"Works with a runner key and for a signed-in person; an agent run acts only inside its own project and is refused.",
			InputSchema: schema([]string{"name"}, map[string]interface{}{
				"name":              str("Project name"),
				"description":       str("Optional description: what the project defines"),
				"parent_project_id": str("Optional ID of the project this one belongs under"),
			}),
			Handler: createProject,
		},
		{
			Name: "set_project_cover",
			Description: "Set a project's cover photo from an image you have: the photo across the top of its Overview and beside it in All projects. " +
				"PNG, JPEG, GIF or WebP, at most 5 MB, as base64 (a data: URL is accepted too); it replaces any cover the project has. " +
				"To make a new image from a description, use generate_project_cover instead. Answers the project, with its cover_updated_at.",
			InputSchema: schema([]string{"project_id", "image_base64"}, map[string]interface{}{
				"project_id":   str("Project ID"),
				"image_base64": str("The image's bytes as base64, or a data:image/...;base64, URL"),
				"mime_type":    str("Optional image/png, image/jpeg, image/gif or image/webp; read from the bytes when left out"),
			}),
			Handler: setProjectCover,
		},
		{
			Name: "generate_project_cover",
			Description: "Generate a photo-like cover for a project from a description, with an image model, and set it, replacing any cover the project has. " +
				"This uses the workspace's own OpenAI API key, so the workspace pays for each one, and without a key it is refused. " +
				"To make a cover yourself, with no key, draw it with draw_project_cover instead, and use this only when asked for a photo. " +
				"Describe a wide picture of the product or its setting, with no text in it. Takes up to a minute; generations are limited per workspace. Answers the project, with its cover_updated_at.",
			InputSchema: schema([]string{"project_id", "prompt"}, map[string]interface{}{
				"project_id": str("Project ID"),
				"prompt":     str("What the picture shows, up to 1,000 characters"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.requestSlow("POST", "/api/v1/projects/"+strArg(args, "project_id")+"/cover/generate",
					map[string]interface{}{"prompt": strArg(args, "prompt")})
				return out, err
			},
		},
		{
			Name:        "remove_project_cover",
			Description: "Remove a project's cover photo. A project with none is not an error. Answers the project.",
			InputSchema: schema([]string{"project_id"}, map[string]interface{}{
				"project_id": str("Project ID"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("DELETE", "/api/v1/projects/"+strArg(args, "project_id")+"/cover", nil, nil)
				return out, err
			},
		},
		{
			Name: "draw_project_cover",
			Description: "Draw a project's cover photo yourself, as SVG, and set it, replacing any cover the project has: the picture across the top of its Overview and beside it in All projects. " +
				"This is the way to make a cover: it needs no image model and no key. The server renders the drawing to a 1536x1024 image. " +
				"Use viewBox=\"0 0 1536 1024\" (another shape is scaled to cover it and cropped). Draw a wide illustration of the product or its setting: " +
				"rect, circle, ellipse, line, polyline, polygon and path (arcs included), grouped in g with transform, filled and stroked with colours " +
				"(#hex, rgb() or names) or linearGradient and radialGradient, with opacity. Text, image, use, filter, mask, clipPath, pattern and dashes are not drawn, so leave them out. " +
				"Fill the whole frame with a background, and keep the subject in the middle: the Overview shows a wide band across the middle of the picture, and All projects a small 3:2 tile. At most 256 KB. A drawing that cannot be rendered is refused with the reason. Answers the project, with its cover_updated_at.",
			InputSchema: schema([]string{"project_id", "svg"}, map[string]interface{}{
				"project_id": str("Project ID"),
				"svg":        str("The drawing: SVG source, starting <svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 1536 1024\">"),
			}),
			Handler: func(c *Client, args map[string]interface{}) (string, error) {
				out, _, err := c.request("POST", "/api/v1/projects/"+strArg(args, "project_id")+"/cover/svg", nil,
					map[string]interface{}{"svg": strArg(args, "svg")})
				return out, err
			},
		},
	}
}

// coverTypes are the image types a cover may be, with the file name each is
// sent under.
var coverTypes = map[string]string{"image/png": "cover.png", "image/jpeg": "cover.jpg", "image/gif": "cover.gif", "image/webp": "cover.webp"}

// setProjectCover answers set_project_cover: the base64 (or data: URL)
// decoded and sent as the upload the app sends, its type the one given or,
// when none is, the one its bytes read as. The server judges it: it checks
// the bytes against the type and refuses anything but the four types.
func setProjectCover(c *Client, args map[string]interface{}) (string, error) {
	raw := strings.TrimSpace(strArg(args, "image_base64"))
	mimeType := strings.TrimSpace(strArg(args, "mime_type"))
	if strings.HasPrefix(raw, "data:") {
		head, data, ok := strings.Cut(raw, ",")
		if !ok || !strings.HasSuffix(head, ";base64") {
			return "", fmt.Errorf("image_base64 is a data: URL that is not base64")
		}
		if mimeType == "" {
			mimeType = strings.TrimSuffix(strings.TrimPrefix(head, "data:"), ";base64")
		}
		raw = data
	}
	img, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(raw), ""))
	if err != nil {
		return "", fmt.Errorf("image_base64 is not base64: %v", err)
	}
	if mimeType == "" {
		mimeType = http.DetectContentType(img)
	}
	// Any other type goes up as it is, for the server to refuse in its words.
	name, ok := coverTypes[mimeType]
	if !ok {
		name = "cover"
	}
	out, _, err := c.uploadFile("/api/v1/projects/"+strArg(args, "project_id")+"/cover", name, mimeType, img)
	return out, err
}

// createProject answers create_project. A project is created and then filed
// under its parent, as the app does, since POST /projects takes no parent.
// When the second step is refused the project exists all the same, so the
// answer says so and carries its id rather than reading as nothing made.
func createProject(c *Client, args map[string]interface{}) (string, error) {
	out, _, err := c.request("POST", "/api/v1/projects", nil, map[string]interface{}{
		"name":        strArg(args, "name"),
		"description": strArg(args, "description"),
	})
	parent := strArg(args, "parent_project_id")
	if err != nil || parent == "" {
		return out, err
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &created); err != nil || created.ID == "" {
		return out, fmt.Errorf("the project was created, but its id could not be read to file it under %s", parent)
	}
	filed, _, err := c.request("PUT", "/api/v1/projects/"+created.ID, nil, map[string]interface{}{
		"parent_project_id": parent,
	})
	if err != nil {
		return out, fmt.Errorf("project %s was created, but could not be filed under %s: %w", created.ID, parent, err)
	}
	return filed, nil
}
