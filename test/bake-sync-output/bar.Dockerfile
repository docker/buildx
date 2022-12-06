FROM busybox
RUN echo bar > /bar

FROM scratch
COPY --from=0 /bar /bar
