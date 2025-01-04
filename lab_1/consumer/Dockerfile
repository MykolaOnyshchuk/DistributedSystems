# syntax=docker/dockerfile:1

FROM golang:1.23

# Set destination for COPY
WORKDIR /app

# Download Go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code. Note the slash at the end, as explained in
# https://docs.docker.com/reference/dockerfile/#copy
COPY *.go ./
ENV PROVIDER_URL="http://load_balancer_p:80/compute"
ENV SECRET_KEY="lab1"
ENV PORT=5050
ENV TOKEN_DURABILITY=420
# Build
RUN CGO_ENABLED=0 GOOS=linux go build -o /lab-1-consumer

# Optional:
# To bind to a TCP port, runtime parameters must be supplied to the docker command.
# But we can document in the Dockerfile what ports
# the application is going to listen on by default.
# https://docs.docker.com/reference/dockerfile/#expose
EXPOSE 5050

# Run
CMD ["/lab-1-consumer"]