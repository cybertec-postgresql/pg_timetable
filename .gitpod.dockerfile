FROM gitpod/workspace-full:latest
# Docker build does not rebuild an image when a base image is changed, increase this counter to trigger it.
ENV TRIGGER_REBUILD=2

# Install PostgreSQL
RUN sudo sh -c 'echo "deb http://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" > /etc/apt/sources.list.d/pgdg.list'
RUN wget --quiet -O - https://www.postgresql.org/media/keys/ACCC4CF8.asc | sudo apt-key add -
RUN sudo apt-get update
RUN sudo apt-get -y install postgresql postgresql-contrib
RUN echo "Check PostgreSQL service is running" \
        i=10 \
        COMMAND='pg_isready' \
        while [ $i -gt 0 ]; do \
            echo "Check PostgreSQL service status" \
            eval $COMMAND && break \
            ((i--)) \
            if [ $i == 0 ]; then \
                echo "PostgreSQL service not ready, all attempts exhausted" \
                exit 1 \
            fi \
            echo "PostgreSQL service not ready, wait 10 more sec, attempts left: $i" \
            sleep 10 \
        done

# Create the PostgreSQL user. 
# Hack with double sudo is because gitpod user cannot run command on behalf of postgres user.
RUN sudo sudo -u postgres psql -c "CREATE USER gitpod PASSWORD 'gitpod' SUPERUSER" -c "CREATE DATABASE gitpod OWNER gitpod"

# This is a bit of a hack. At the moment we have no means of starting background
# tasks from a Dockerfile. This workaround checks, on each bashrc eval, if the
# PostgreSQL server is running, and if not starts it.
RUN printf "\n# Auto-start PostgreSQL server.\nsudo service postgresql start\n" >> ~/.bashrc