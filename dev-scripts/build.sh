#!/usr/bin/env bash

function build() {
    local ret_value=""
    default_image_name
    DEFAULT_IMAGE_NAME=$ret_value
    
    local IMAGE_NAME="${1:-$DEFAULT_IMAGE_NAME}"
    docker rmi -f "$IMAGE_NAME" || true
    
    msg "Image build..."
    docker build --no-cache -t "$IMAGE_NAME" -f build/linux/Dockerfile .
    
    msg "Image $IMAGE_NAME is built"
}