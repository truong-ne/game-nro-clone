FROM eclipse-temurin:21-jdk-jammy
WORKDIR /app
COPY SRC/20.jar /app/20.jar
COPY SRC/data /app/data
COPY SRC/maintenanceConfig.txt /app/maintenanceConfig.txt
RUN cp /app/data/map/tile_set_Info /app/data/map/tile_set_info && mkdir -p /app/log /app/admin
EXPOSE 14445
CMD ["java", "-Xms256m", "-Xmx2g", "-Dfile.encoding=UTF-8", "-Duser.timezone=Asia/Ho_Chi_Minh", "-jar", "20.jar"]
