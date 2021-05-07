FROM golang:1.13.8 as builder

RUN mkdir /build
COPY . /build/

WORKDIR /build
RUN go build -mod=vendor .

FROM keybaseio/client:5.7.0-20210126121847-cfdcb9f95f-slim

COPY --from=builder /build/kbam /usr/bin/kbam
COPY default.tmpl .

RUN useradd --create-home --shell /bin/bash kbam

USER kbam

ENTRYPOINT [ "/usr/bin/kbam" ]