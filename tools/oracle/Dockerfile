# Oracle LibreOffice (P1-07, D28). Image de base épinglée par digest.
FROM debian:bookworm-slim@sha256:99c0b25c46d0214d5a2091092e7908907dcaecd6a962cde0ac700acee222b6f3

ENV DEBIAN_FRONTEND=noninteractive LC_ALL=C.UTF-8 LANG=C.UTF-8 TZ=UTC
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      libreoffice-calc=4:7.4.7-1+deb12u14 \
      python3-uno=4:7.4.7-1+deb12u14 \
      python3-venv fonts-dejavu-core \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /oracle
COPY requirements.txt .
RUN python3 -m venv --system-site-packages /opt/venv \
 && /opt/venv/bin/pip install --no-cache-dir -r requirements.txt
ENV PATH=/opt/venv/bin:$PATH

COPY *.py ./
# Le gabarit et la sortie sont montés par `make oracle` sous /data.
ENTRYPOINT ["python3", "oracle.py"]
CMD ["--template", "/data/xlsx/simply_investing_template.xlsx", "--out", "/data/oracle/cases.json"]
