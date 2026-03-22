# Deployment

BunGo eliminates vendor chains by abstracting HTTP handlers underneath `bungo.Request` constructs. To deploy BunGo to highly specialized environments, you switch the parent `Engine`.

There are multiple engines available that plug directly into the initial `bungo.NewServer` invocation!

## 1. Native HTTP Environment
Ideal for generic Docker deployments or long running servers (VPS).
```go
import "github.com/piotr-nierobisz/BunGo/engine"

func main() {
    engineInstance := engine.NewHTTPEngine()
    srv := bungo.NewServer(engineInstance, "./web")
    // ...
    srv.Serve(3303)
}
```

## 2. Google Cloud Functions
A specialized adapter converting Cloud Function traffic seamlessly into your BunGo Router! 
```bash
go get github.com/piotr-nierobisz/BunGo/engine/gcp
```

```go
import "github.com/piotr-nierobisz/BunGo/engine/gcp"

func main() {
    // Note: The String parameter MUST match the target gcloud entrypoint exactly.
    gcpEngine := engine_gcp.NewGCPEngine("MyCloudFunction")  
    srv := bungo.NewServer(gcpEngine, "./web")
    // ...
    srv.Serve(8080)
}
```

## 3. AWS Lambda
A specialized adapter mapping AWS Lambda Event Payloads directly to your Views!
```bash
go get github.com/piotr-nierobisz/BunGo/engine/aws
```

```go
import "github.com/piotr-nierobisz/BunGo/engine/aws"

func main() {
    awsEngine := engine_aws.NewLambdaEngine()
    srv := bungo.NewServer(awsEngine, "./web")
    // ...
    srv.Serve(8080) // port value is ignored: LambdaEngine runs lambda.Start and does not ListenAndServe
}
```
For this engine, `Serve`’s port is unused (any value is acceptable). For local testing, use the AWS SAM CLI or Lambda Runtime Interface Emulator (RIE).

If you use AI-assisted development tools, the [AI Agent Reference](./ai-guide.md) provides a self-contained rules text you can copy into your project to give coding agents full context on BunGo's architecture and conventions.