# Project: Nomad Cloud Hypervisor Plugin
This project is cloud hypervisor plugin for HashiCorp Nomad, written in Go.

## Technical Context
- **Language:** Go (latest stable).
- **Core Library:** `github.com/hashicorp/nomad/plugins/drivers`.
- **Framework:** Uses the Nomad Task Driver V2 API.
- **Protocol:** Works over gRPC via HashiCorp's `go-plugin`.

## Driver Responsibilities
- Implement the `Driver` interface (StartTask, StopTask, DestroyTask, InspectTask, etc.).
- Handle task configuration schemas via `hclspec`.
- Manage task resource isolation (fingerprinting, CPU/RAM limits).
- Provide streaming logs and statistics.

## Coding Style & Rules
- **Conciseness:** Do not explain Nomad concepts unless asked. Just provide the code or the fix.
- **Error Handling:** Use Nomad's internal error types where appropriate. Ensure `ctx` is respected in all long-running operations.
- **Concurrency:** Be extremely careful with go-routines inside `StartTask`; ensure they are cleaned up in `StopTask`.
- **Boilerplate:** When creating new methods, ensure they satisfy the driver interfaces exactly.

## Project Structure
- `chdriver/`: Contains the main driver implementation.
- `main.go`: The plugin entry point.
- `example/`: Nomad job files for testing.

## Interaction Rule
- Do not repeat these instructions.
- If I ask for a new feature, prioritize looking at existing HCL schema definitions in the project before suggesting changes.
