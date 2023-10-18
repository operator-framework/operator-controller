FROM alpine as build
ARG env_plain_bundle_image
ADD test-catalog /configs
RUN sed -i "/image: PLAIN_BUNDLE_IMAGE_URL/c\image: $env_plain_bundle_image" configs/catalog.yaml

FROM scratch
COPY --from=build /configs /configs
