#!/bin/bash

EXECUTE_DIR=$(dirname "$0")
DIR=/tmp/outputs/7261da4b-9c45-4619-bfb0-a6119dc7b6e6/360p
cat $DIR/playlist.m3u8 > ./$EXECUTE_DIR/playlist.m3u8
ls $DIR/ > ./$EXECUTE_DIR/list.txt