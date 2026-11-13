FROM busybox:1.36
ARG SHA=dev
RUN mkdir -p /www && printf '<h1>Quill pipeline showcase</h1><p>commit %s</p>\n' "$SHA" > /www/index.html
EXPOSE 8080
CMD ["httpd", "-f", "-p", "8080", "-h", "/www"]
