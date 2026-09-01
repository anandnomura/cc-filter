package com.company.bap.apipep;

import tools.jackson.databind.JsonNode;
import reactor.core.publisher.Mono;

interface GrantConsumer {
    Mono<ConsumedGrant> consume(String token, JsonNode operation);
}

record ConsumedGrant(boolean consumed, String grantId) {}
