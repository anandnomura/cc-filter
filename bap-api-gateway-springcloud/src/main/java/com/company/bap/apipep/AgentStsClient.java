package com.company.bap.apipep;

import com.fasterxml.jackson.annotation.JsonProperty;
import tools.jackson.databind.JsonNode;
import io.netty.handler.ssl.SslContextBuilder;
import java.io.File;
import java.util.Map;
import org.springframework.http.HttpHeaders;
import org.springframework.http.MediaType;
import org.springframework.stereotype.Component;
import org.springframework.web.reactive.function.client.WebClient;
import reactor.core.publisher.Mono;
import reactor.netty.http.client.HttpClient;
import org.springframework.http.client.reactive.ReactorClientHttpConnector;

@Component
final class AgentStsClient implements GrantConsumer {
    private final WebClient client;
    private final String apiKey;

    AgentStsClient(BapPepProperties properties, WebClient.Builder builder) throws Exception {
        this.apiKey = System.getenv(properties.agentStsApiKeyEnv());
        if (apiKey == null || apiKey.isBlank()) {
            throw new IllegalStateException("API PEP Agent STS credential is unavailable in " + properties.agentStsApiKeyEnv());
        }
        if (properties.agentStsCaPath() != null && !properties.agentStsCaPath().isBlank()) {
            var ssl = SslContextBuilder.forClient().trustManager(new File(properties.agentStsCaPath())).build();
            builder.clientConnector(new ReactorClientHttpConnector(HttpClient.create().secure(spec -> spec.sslContext(ssl))));
        }
        this.client = builder.baseUrl(properties.agentStsUrl()).build();
    }

    @Override
    public Mono<ConsumedGrant> consume(String token, JsonNode operation) {
        return client.post()
                .uri("/bap/v1/agent-sts/consume")
                .header(HttpHeaders.AUTHORIZATION, "Bearer " + apiKey)
                .contentType(MediaType.APPLICATION_JSON)
                .bodyValue(Map.of("agent_grant", token, "operation", operation))
                .retrieve()
                .bodyToMono(ConsumeResponse.class)
                .filter(response -> response.consumed && response.grantId != null && !response.grantId.isBlank())
                .switchIfEmpty(Mono.error(new IllegalStateException("Agent STS did not confirm consumption")))
                .map(response -> new ConsumedGrant(true, response.grantId));
    }

    private record ConsumeResponse(boolean consumed, @JsonProperty("grant_id") String grantId) {}
}
