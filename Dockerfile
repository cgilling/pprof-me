FROM golang:1.10.4
WORKDIR /go/src/github.com/cgilling/pprof-me
RUN CGO_ENABLED=0 GOOS=linux go get github.com/google/pprof
COPY . ./

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o pprof-me .

FROM alpine:latest  
RUN apk --no-cache add ca-certificates graphviz binutils
WORKDIR /root/
COPY --from=0 /go/src/github.com/cgilling/pprof-me/pprof-me .
COPY --from=0 /go/bin/pprof /bin/
CMD ["./pprof-me"]  