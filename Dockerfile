FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY dist/manager /manager
USER 65532:65532
ENTRYPOINT ["/manager"]
