FROM maven:3.9-eclipse-temurin-17 AS build
WORKDIR /src
COPY settings.xml /root/.m2/settings.xml
COPY pom.xml .
COPY src src
RUN mvn -q -DskipTests package

FROM eclipse-temurin:17-jre-alpine
RUN addgroup -S proofcode && adduser -S proofcode -G proofcode
USER proofcode
COPY --from=build /src/target/control-plane-0.4.0.jar /app.jar
ENTRYPOINT ["java", "-XX:MaxRAMPercentage=75", "-jar", "/app.jar"]
