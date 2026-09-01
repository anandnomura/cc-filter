package com.company.bap.apipep;

import tools.jackson.databind.JsonNode;
import tools.jackson.databind.ObjectMapper;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.ArrayList;
import java.util.Collections;
import org.springframework.stereotype.Component;

@Component
final class EnvelopeValidator {
    static final String API_TOOL = "mcp__bap_gateway__execute";
    private final ObjectMapper mapper;

    EnvelopeValidator(ObjectMapper mapper) { this.mapper = mapper; }

    void validate(AgentGrantEnvelope envelope, String expectedMethod, String expectedUrl) {
        if (envelope == null || blank(envelope.agentGrant()) || envelope.operation() == null || blank(envelope.method()) || blank(envelope.url())) {
            throw new IllegalArgumentException("trusted BAP API envelope is incomplete");
        }
        if (!expectedMethod.equalsIgnoreCase(envelope.method()) || !expectedUrl.equals(envelope.url())) {
            throw new IllegalArgumentException("business request is not assigned to this protected route");
        }
        JsonNode operation = envelope.operation();
        JsonNode properties = operation.path("resource").path("properties");
        if (!"gateway.execute".equals(operation.path("action").path("name").asText())
                || !API_TOOL.equals(properties.path("tool").asText())
                || !expectedMethod.equalsIgnoreCase(properties.path("httpMethod").asText())
                || !expectedUrl.equals(properties.path("target").asText())) {
            throw new IllegalArgumentException("business request does not match the AgentGrant operation");
        }
        String digest = "sha256:" + sha256(canonical(envelope.body()));
        if (!digest.equals(properties.path("bodyDigest").asText())) {
            throw new IllegalArgumentException("business body does not match the AgentGrant operation");
        }
    }

    byte[] backendBody(AgentGrantEnvelope envelope) {
        return canonical(envelope.body()).getBytes(StandardCharsets.UTF_8);
    }

    private String canonical(JsonNode node) {
        if (node == null || node.isNull()) return "null";
        if (node.isArray()) {
            var parts = new ArrayList<String>();
            node.forEach(value -> parts.add(canonical(value)));
            return "[" + String.join(",", parts) + "]";
        }
        if (node.isObject()) {
            var names = new ArrayList<String>();
            names.addAll(node.propertyNames());
            Collections.sort(names);
            var parts = new ArrayList<String>();
            for (String name : names) parts.add(jsonString(name) + ":" + canonical(node.get(name)));
            return "{" + String.join(",", parts) + "}";
        }
        try { return mapper.writeValueAsString(node); }
        catch (Exception error) { throw new IllegalArgumentException("business body is not JSON-compatible", error); }
    }

    private String jsonString(String value) {
        try { return mapper.writeValueAsString(value); }
        catch (Exception error) { throw new IllegalArgumentException("invalid JSON property", error); }
    }

    private static String sha256(String value) {
        try { return java.util.HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(value.getBytes(StandardCharsets.UTF_8))); }
        catch (Exception error) { throw new IllegalStateException(error); }
    }
    private static boolean blank(String value) { return value == null || value.isBlank(); }
}
