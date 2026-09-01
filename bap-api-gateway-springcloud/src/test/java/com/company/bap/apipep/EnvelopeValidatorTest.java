package com.company.bap.apipep;

import static org.junit.jupiter.api.Assertions.assertDoesNotThrow;
import static org.junit.jupiter.api.Assertions.assertThrows;
import tools.jackson.databind.ObjectMapper;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import org.junit.jupiter.api.Test;

class EnvelopeValidatorTest {
    private final ObjectMapper mapper = new ObjectMapper();
    private final EnvelopeValidator validator = new EnvelopeValidator(mapper);

    @Test
    void exactEnvelopePassesAndChangedBodyFails() throws Exception {
        String body = "{\"release\":\"2026.08\"}";
        String digest = "sha256:" + java.util.HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(body.getBytes(StandardCharsets.UTF_8)));
        var operation = mapper.readTree("""
            {"subject":{"type":"agent","id":"claude-code-local"},"action":{"name":"gateway.execute"},
             "resource":{"type":"tool-invocation","id":"r","properties":{"tool":"mcp__bap_gateway__execute","httpMethod":"POST","target":"https://api.staging.company.example/orders/deploy","bodyDigest":"%s"}},"context":{}}
            """.formatted(digest));
        var valid = new AgentGrantEnvelope("POST", "https://api.staging.company.example/orders/deploy", mapper.readTree(body), "opaque", operation);
        assertDoesNotThrow(() -> validator.validate(valid, "POST", "https://api.staging.company.example/orders/deploy"));
        var changed = new AgentGrantEnvelope(valid.method(), valid.url(), mapper.readTree("{\"release\":\"changed\"}"), valid.agentGrant(), operation);
        assertThrows(IllegalArgumentException.class, () -> validator.validate(changed, "POST", valid.url()));
    }

    @Test
    void arbitraryDestinationFails() throws Exception {
        var envelope = new AgentGrantEnvelope("POST", "https://evil.example/delete", mapper.readTree("{}"), "opaque", mapper.readTree("{}"));
        assertThrows(IllegalArgumentException.class, () -> validator.validate(envelope, "POST", "https://api.staging.company.example/orders/deploy"));
    }
}
