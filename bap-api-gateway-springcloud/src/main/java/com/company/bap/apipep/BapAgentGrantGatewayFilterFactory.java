package com.company.bap.apipep;

import tools.jackson.databind.ObjectMapper;
import java.net.URI;
import java.util.List;
import org.reactivestreams.Publisher;
import org.springframework.cloud.gateway.filter.GatewayFilter;
import org.springframework.cloud.gateway.filter.factory.AbstractGatewayFilterFactory;
import org.springframework.core.io.buffer.DataBuffer;
import org.springframework.core.io.buffer.DataBufferUtils;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpMethod;
import org.springframework.http.MediaType;
import org.springframework.http.server.reactive.ServerHttpRequestDecorator;
import org.springframework.stereotype.Component;
import org.springframework.web.server.ResponseStatusException;
import reactor.core.publisher.Flux;
import static org.springframework.http.HttpStatus.FORBIDDEN;
import static org.springframework.http.HttpStatus.SERVICE_UNAVAILABLE;

@Component
public final class BapAgentGrantGatewayFilterFactory extends AbstractGatewayFilterFactory<BapAgentGrantGatewayFilterFactory.Config> {
    private final ObjectMapper mapper;
    private final EnvelopeValidator validator;
    private final GrantConsumer consumer;

    public BapAgentGrantGatewayFilterFactory(ObjectMapper mapper, EnvelopeValidator validator, GrantConsumer consumer) {
        super(Config.class);
        this.mapper = mapper;
        this.validator = validator;
        this.consumer = consumer;
    }

    @Override
    public List<String> shortcutFieldOrder() { return List.of("externalUrl", "method", "backendPath", "backendApiKeyEnv"); }

    @Override
    public GatewayFilter apply(Config config) {
        return (exchange, chain) -> DataBufferUtils.join(exchange.getRequest().getBody()).flatMap(buffer -> {
            byte[] bytes = new byte[buffer.readableByteCount()];
            buffer.read(bytes);
            DataBufferUtils.release(buffer);
            final AgentGrantEnvelope envelope;
            try {
                envelope = mapper.readValue(bytes, AgentGrantEnvelope.class);
                validator.validate(envelope, config.method, config.externalUrl);
            } catch (Exception error) {
                return reactor.core.publisher.Mono.error(new ResponseStatusException(FORBIDDEN, "BAP API envelope denied"));
            }
            return consumer.consume(envelope.agentGrant(), envelope.operation())
                    .onErrorMap(error -> new ResponseStatusException(SERVICE_UNAVAILABLE, "Agent STS consumption failed"))
                    .flatMap(consumed -> {
                        byte[] backendBody = validator.backendBody(envelope);
                        String backendKey = System.getenv(config.backendApiKeyEnv);
                        if (backendKey == null || backendKey.isBlank()) return reactor.core.publisher.Mono.error(new ResponseStatusException(SERVICE_UNAVAILABLE, "backend identity unavailable"));
                        var base = exchange.getRequest().mutate().method(HttpMethod.valueOf(config.method)).uri(rewritePath(exchange.getRequest().getURI(), config.backendPath));
                        base.headers(headers -> {
                            String traceparent = headers.getFirst("traceparent");
                            headers.clear();
                            headers.setContentType(MediaType.APPLICATION_JSON);
                            headers.setContentLength(backendBody.length);
                            headers.set(HttpHeaders.AUTHORIZATION, "Bearer " + backendKey);
                            headers.set("Idempotency-Key", consumed.grantId());
                            if (traceparent != null) headers.set("traceparent", traceparent);
                        });
                        var decorated = new ServerHttpRequestDecorator(base.build()) {
                            @Override public Flux<DataBuffer> getBody() { return Flux.just(exchange.getResponse().bufferFactory().wrap(backendBody)); }
                        };
                        return chain.filter(exchange.mutate().request(decorated).build());
                    });
        });
    }

    private static URI rewritePath(URI source, String path) {
        try { return new URI(source.getScheme(), source.getUserInfo(), source.getHost(), source.getPort(), path, null, null); }
        catch (Exception error) { throw new IllegalArgumentException("invalid protected backend path", error); }
    }

    public static final class Config {
        private String externalUrl;
        private String method;
        private String backendPath;
        private String backendApiKeyEnv;
        public String getExternalUrl() { return externalUrl; }
        public void setExternalUrl(String value) { externalUrl = value; }
        public String getMethod() { return method; }
        public void setMethod(String value) { method = value; }
        public String getBackendPath() { return backendPath; }
        public void setBackendPath(String value) { backendPath = value; }
        public String getBackendApiKeyEnv() { return backendApiKeyEnv; }
        public void setBackendApiKeyEnv(String value) { backendApiKeyEnv = value; }
    }
}
