FROM golang:1.9.4
WORKDIR /go/src/github.com/cgilling/pprof-me
RUN CGO_ENABLED=0 GOOS=linux go get github.com/google/pprof
RUN go get -d -v \
				 github.com/julienschmidt/httprouter \
				 github.com/kelseyhightower/envconfig \
				 github.com/kennygrant/sanitize \
				 github.com/pborman/uuid
COPY *.go ./

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o pprof-me .

FROM alpine:latest  
RUN apk --no-cache add ca-certificates graphviz binutils
WORKDIR /root/
COPY --from=0 /go/src/github.com/cgilling/pprof-me/pprof-me .
COPY --from=0 /go/bin/pprof /bin/
# These two lines are for testing only
COPY *.profile /root/
COPY nos_binary /root/
CMD ["./pprof-me"]  