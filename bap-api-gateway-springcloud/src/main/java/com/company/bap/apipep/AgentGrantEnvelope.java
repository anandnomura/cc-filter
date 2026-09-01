package com.company.bap.apipep;

import com.fasterxml.jackson.annotation.JsonProperty;
import tools.jackson.databind.JsonNode;

public record AgentGrantEnvelope(
        String method,
        String url,
        JsonNode body,
        @JsonProperty("_bap_agent_grant") String agentGrant,
        @JsonProperty("_bap_operation") JsonNode operation) {}
