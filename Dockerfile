FROM golang:1.26.5-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/vivek/agent-task-tracker/internal/buildinfo.Version=${VERSION} -X github.com/vivek/agent-task-tracker/internal/buildinfo.Commit=${COMMIT} -X github.com/vivek/agent-task-tracker/internal/buildinfo.BuildDate=${BUILD_DATE}" -o /forge ./cmd/forge

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /forge /forge
USER nonroot:nonroot
ENTRYPOINT ["/forge"]
