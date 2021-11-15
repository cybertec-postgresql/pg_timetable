FROM gitpod/workspace-full:latest
# Docker build does not rebuild an image when a base image is changed, increase this counter to trigger it.
ENV TRIGGER_REBUILD=2

# Install PostgreSQL
RUN sudo sh -c 'echo "deb http://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" > /etc/apt/sources.list.d/pgdg.list'
RUN wget --quiet -O - https://www.postgresql.org/media/keys/ACCC4CF8.asc | sudo apt-key add -
RUN sudo apt-get update
RUN sudo apt-get -y install postgresql postgresql-contrib

# Check PostgreSQL service is running
RUN sudo service postgresql start \
    && until pg_isready; do sleep 1; done \
    # Create the PostgreSQL user. 
    # Hack with double sudo is because gitpod user cannot run command on behalf of postgres user.
    && sudo sudo -u postgres psql \
        -c "CREATE USER gitpod PASSWORD 'gitpod' SUPERUSER" \
        -c "CREATE DATABASE gitpod OWNER gitpod"

# This is a bit of a hack. At the moment we have no means of starting background
# tasks from a Dockerfile. This workaround checks, on each bashrc eval, if the
# PostgreSQL server is running, and if not starts it.
RUN printf "\n# Auto-start PostgreSQL server.\nsudo service postgresql start\n" >> ~/.bashrc