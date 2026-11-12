# laya

Standalone Go module for generic ollaya decisions and pinned model artifacts.

```go
manifest := laya.FixedManifest()
client := laya.Client{BaseURL: "http://127.0.0.1:11435", APIKey: key, Observer: observe}
result, err := client.Classify(ctx, laya.Request{
    Model: manifest.Name,
    State: map[string]any{"signal": "fixed, non-sensitive input"},
    Questions: map[string]laya.Question{
        "category": {Type: laya.AnswerChoice, Instructions: "Classify this input",
            Criteria: map[string]string{"a": "first", "b": "second"}},
    },
})
```

The host defines decision meaning and sanitizes state before sending it. Installation, identity, storage, resource enforcement, downloads, and service management are host adapters. `FixedManifest` pins the official runtime/model versions and known model file digests.

`DockerRuntime` provides `Ensure`, `Status`, `Stop`, and `Remove` through an injected privileged `DockerExecute` adapter. Pass an explicit `DockerSpec` and `DockerPolicy`; the adapter receives only fixed argument lists. Existing containers must match the approved digest, identity, mounts, loopback port and resource/isolation settings before they can be started or removed. Removal preserves the model volume. A running container is a process status; use the decision client separately to verify that the requested model is ready.

The package never connects to a Docker socket. The host owns daemon access, approval, bounded command output and `ErrContainerMissing` normalization. Clients compile independently of the host's operating system; the host must reject unsupported hosting platforms.
