FROM alpine:3.11.2

RUN apk add python3 gcc musl-dev python3-dev postgresql-dev libffi-dev
RUN ln -s /usr/bin/python3 /usr/bin/python
RUN pip3 install --upgrade pip
WORKDIR /app
COPY static/ static/
COPY templates/ templates/
COPY env.py server.py requirements.txt ./
RUN pip install -r requirements.txt
ENV FLASK_APP server.py

EXPOSE 5000

ENTRYPOINT ["flask", "run"]
