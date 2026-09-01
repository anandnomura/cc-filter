package com.company.bap.apipep;

import org.springframework.boot.context.properties.ConfigurationProperties;

@ConfigurationProperties("bap.pep")
public record BapPepProperties(String agentStsUrl, String agentStsApiKeyEnv, String agentStsCaPath, String resource) {}
