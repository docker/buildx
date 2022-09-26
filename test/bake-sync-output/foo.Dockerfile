FROM busybox
RUN echo foo > /foo

FROM scratch
COPY --from=0 /foo /foo
