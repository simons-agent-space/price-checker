FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /price-checker ./cmd/price-checker

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /price-checker /price-checker
USER nonroot:nonroot
EXPOSE 3000
ENTRYPOINT ["/price-checker"]
