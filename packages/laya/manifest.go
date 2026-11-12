package laya

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"strings"
)

const OllayaVersion = "v0.18.0"
const RuntimeCommit = "a8bce962dc8219e696604403a5c2a30e6c9523f0"
const ModelCommit = "aa8c91ca088ec597df95a0d1c76b3063cb2ae5e8"
const ModelManifestSHA256 = "2840506e1f978aeb696a6f84af43ecc7fca95754cb63a67bafe6132532d384eb"
const DefaultModel = "laya:multilingual"

//go:embed multilingual.json
var fixedModels embed.FS

type File struct {
	Name, URL string
	Size      int64
	SHA256    string
}
type Model struct {
	Name, SourceCommit string
	Files              []File
	MinimumMemory      int64
	Manifest           []byte
	ManifestPath       string
}

type modelDescriptor struct {
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

// A manifest activates only immutable, content-addressed blobs. This lets a
// failed publication leave the previously active manifest usable.
func validateModelManifest(model Model) error {
	if len(model.Manifest) == 0 {
		return nil
	}
	if len(model.Manifest) > 1<<20 || !strings.HasPrefix(model.ManifestPath, "manifests/") {
		return errors.New("invalid model manifest")
	}
	var doc struct {
		SchemaVersion int               `json:"schemaVersion"`
		Config        modelDescriptor   `json:"config"`
		Layers        []modelDescriptor `json:"layers"`
	}
	if json.Unmarshal(model.Manifest, &doc) != nil || doc.SchemaVersion != 2 || len(doc.Layers) == 0 {
		return errors.New("invalid model manifest")
	}
	files := make(map[string]File, len(model.Files))
	for _, file := range model.Files {
		files[file.Name] = file
	}
	seen := map[string]bool{}
	for _, item := range append([]modelDescriptor{doc.Config}, doc.Layers...) {
		if !strings.HasPrefix(item.Digest, "sha256:") {
			return errors.New("invalid manifest digest")
		}
		digest := strings.TrimPrefix(item.Digest, "sha256:")
		name := "blobs/sha256-" + digest
		file, ok := files[name]
		if !ok || digest != strings.ToLower(file.SHA256) || item.Size != file.Size || seen[name] {
			return errors.New("manifest artifact mismatch")
		}
		seen[name] = true
	}
	if len(seen) != len(files) {
		return errors.New("unreferenced model artifact")
	}
	return nil
}

// FixedManifest returns every artifact in the official version-pinned model.
func FixedManifest() Model {
	raw, _ := fixedModels.ReadFile("multilingual.json")
	// Preserve the registry's exact bytes across Windows text checkouts.
	raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	type descriptor struct {
		Digest string   `json:"digest"`
		Size   int64    `json:"size"`
		URLs   []string `json:"urls"`
	}
	var doc struct {
		Config descriptor   `json:"config"`
		Layers []descriptor `json:"layers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		panic(err)
	}
	model := Model{Name: DefaultModel, SourceCommit: ModelCommit, MinimumMemory: 2 << 30, Manifest: raw, ManifestPath: "manifests/ollaya.dev/library/laya/multilingual"}
	for _, item := range append([]descriptor{doc.Config}, doc.Layers...) {
		digest := item.Digest[len("sha256:"):]
		model.Files = append(model.Files, File{Name: "blobs/sha256-" + digest, URL: item.URLs[0], Size: item.Size, SHA256: digest})
	}
	return model
}
