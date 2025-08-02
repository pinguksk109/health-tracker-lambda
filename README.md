# Go

## Run Locally

To run the Lambda function locally using the AWS Lambda Runtime Interface Emulator (RIE):

### 1. Build for Linux/ARM64

```bash
GOOS=linux GOARCH=arm64 go build -o bootstrap main.go
```

### 2. Download RIE (only needed once)

```bash
curl -Lo aws-lambda-rie https://github.com/aws/aws-lambda-runtime-interface-emulator/releases/latest/download/aws-lambda-rie
chmod +x aws-lambda-rie
```

### 3. Run locally with emulator

```bash
./aws-lambda-rie ./bootstrap
```

### 4. Invoke with test event (from another terminal)

```bash
curl -XPOST "http://localhost:8080/2015-03-31/functions/function/invocations" \
  -d '{"body": "test message"}'
```

---

## Deploy to AWS Lambda

```bash
GOOS=linux GOARCH=arm64 go build -o bootstrap main.go
zip function.zip bootstrap
```

---
