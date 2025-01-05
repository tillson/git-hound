# Use the official Golang image as a parent image
FROM golang:latest

# Set the working directory
WORKDIR /app

# Install git-hound
RUN git clone https://github.com/tillson/git-hound.git
RUN apt-get install libpcre3-dev
RUN cd git-hound && go build -o /usr/local/bin/git-hound

# Copy the locally required files to the container
COPY . .

# Set up a directory for .githound
RUN mkdir -p /root/.githound

# Set up volume for input files
VOLUME /data
VOLUME /root/.githound

# Set the default command for the container
ENTRYPOINT ["git-hound"]
