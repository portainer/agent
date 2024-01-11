# Using Tilt (tilt.dev) with the Portainer Agent

## Directory Contents
This directory contains some tiltfiles for use with Tilt.

## Getting started for developers
This allows you to edit the agent code and have it live reload from within a container running in your local environment.
It supports both docker and kubernetes.

The top level project directory already contains the docker tiltfile named as "Tiltfile".
Copy .env.example to .env
Edit .env as desired.

e.g.  To run the agent in edge mode with insecure poll, something like the following environment variables would work.
Adjust your edge_key and edge_id accordingly

    ----- .env contents ----
    LOG_LEVEL=DEBUG
    TZ=Pacific/Auckland
    EDGE=1
    EDGE_ID=<your edge id>
    EDGE_KEY=<your edge key>
    EDGE_INSECURE_POLL=1
    ----- end of .env contents ----

Note: The docker-compose file I have maps the timezone database.  So if you need it to test scheduling then set the TZ variable.
It should match one of the directories below "/usr/share/zoneinfo".   This may or may not be the correct path for OSX but is on Ubuntu.

Ensure you have the correct 3rd party binaries in the dist directory first by running `make download-binaries`.

When you're ready to run the agent, just simply run in the terminal `tilt up`.  This will compile everything and bring the container up.

Now edit the agent code in your preferred editor and note when you save that it will build and restart the container for you.

Regarding the kubernetes tiltfile.  This is as yet untested, but in theory should work.

As this is all a work in progress, expect our tilt scripts and files to evolve over time.
