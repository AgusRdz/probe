FROM golang:1.24-alpine

RUN apk add --no-cache git gcc musl-dev

WORKDIR /app

COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true

COPY . .

CMD ["go", "build", "-o", "bin/probe", "."]
