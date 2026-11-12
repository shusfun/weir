# dsh

Standalone Go module for hosting the official DeepSeek Harness package. It has no dependency on the Weir root module.

```go
in := dsh.DefaultInstaller("/var/lib/example/dsh/runtime")
in.Namespace = "example"
in.Download = downloadAdapter // optional: bridge a host relay/resumable downloader
installed, err := in.Install(ctx, version)

registry, err := dsh.NewRegistry(observer,
    dsh.Tool{Name: "status.read", Description: "Read safe status", InputSchema: schema,
        Sources: []string{"console", "analysis"}, Secret: []string{"api_key"},
        Handle: readStatus},
    dsh.DraftTool("draft.submit", "Submit a proposal", draftSchema,
        []string{"console"}, hostDraftReceiver),
)
handler := dsh.MCPHandler(dsh.MCPConfig{
    Name: "example", Version: "1", Instructions: "Host instructions",
    Authenticate: authenticateSource, Registry: func(context.Context, string) (*dsh.Registry, error) {
        return registry, nil
    },
})
```

The host owns authentication, the loopback listener, permissions, persistence, authorization, draft previews, and approval. Secret fields are omitted from discovery and rejected from model calls. Tool and lifecycle observers receive metadata only, never prompts, credentials, or arguments.

Background jobs require an absolute runtime directory and a caller-supplied compatibility policy. `Job.Run` serializes each namespace across processes, includes lock waiting in its timeout, and removes the ephemeral profile patch before releasing the lock. Sessions and keys are retained. The managed agent calls `RecoverBackgroundProfiles(ctx, paths)` before accepting work after a restart; standalone supervisors should do the same to remove patches left by interrupted jobs. Recovery never executes a task or approves a draft.
