# Elasticsearch 单节点镜像：官方镜像 + 版本严格匹配的 IK 中文分词插件。
# 升级 ES 版本时，必须同步修改 IK_VERSION 为相同版本号。
ARG ES_VERSION=8.15.0
FROM docker.elastic.co/elasticsearch/elasticsearch:${ES_VERSION}

ARG ES_VERSION=8.15.0

# analysis-ik 现由 InfiniLabs 维护；--batch 跳过安装确认。
RUN bin/elasticsearch-plugin install --batch \
    "https://release.infinilabs.com/analysis-ik/stable/elasticsearch-analysis-ik-${ES_VERSION}.zip"
