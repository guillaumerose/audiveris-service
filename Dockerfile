FROM golang AS src
WORKDIR /app
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY cmd/ .
RUN go build -o /service .

FROM debian:stretch-slim

RUN apt-get update && apt-get install -y  \
        curl \
        wget \
        git \
        tesseract-ocr \
        tesseract-ocr-eng \
        tesseract-ocr-deu \
        tesseract-ocr-fra \
        musescore

RUN wget https://download.oracle.com/java/17/latest/jdk-17_linux-x64_bin.deb \
        && apt install -y ./jdk-17_linux-x64_bin.deb \
        && rm ./jdk-17_linux-x64_bin.deb

ENV JAVA_HOME=/usr/lib/jvm/jdk-17/
ENV PATH=$PATH:$JAVA_HOME/bin

COPY check-version.patch /check-version.patch
RUN  git clone --branch development https://github.com/Audiveris/audiveris.git && \
        cd audiveris && \
        git apply --ignore-whitespace /check-version.patch && \
        ./gradlew build && \
        mkdir /audiveris-extract && \
        tar -xvf /audiveris/build/distributions/Audiveris*.tar -C /audiveris-extract && \
        mv /audiveris-extract/Audiveris*/* /audiveris-extract/ &&\
        rm -r /audiveris

# For musescore
ENV QT_QPA_PLATFORM=offscreen

COPY --from=src /service /service
COPY public /public

EXPOSE 1323
CMD /service
