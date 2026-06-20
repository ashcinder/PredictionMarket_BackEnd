package com.example.brokerfi.xc.agent.gold.model.logic;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

public final class GoldMarketSecurityPolicy {
    public static final String DEFAULT_CONTRACT_ADDRESS = "0xad4F9eD0F2b51A26314C9f83DF588cCcE26ae03c";

    private GoldMarketSecurityPolicy() {
    }

    public static boolean isDeveloperMarketToolsEnabled(boolean buildDebug) {
        return buildDebug;
    }

    public static String resolveContractAddress(boolean developerToolsEnabled, String savedAddress) {
        return resolveContractAddresses(developerToolsEnabled, savedAddress).get(0);
    }

    public static List<String> resolveContractAddresses(boolean developerToolsEnabled, String savedAddresses) {
        if (!developerToolsEnabled) {
            return Collections.singletonList(DEFAULT_CONTRACT_ADDRESS);
        }
        List<String> addresses = parseContractAddresses(savedAddresses);
        if (addresses.isEmpty()) {
            return Collections.singletonList(DEFAULT_CONTRACT_ADDRESS);
        }
        return addresses;
    }

    public static List<String> parseContractAddresses(String rawAddresses) {
        List<String> addresses = new ArrayList<>();
        if (rawAddresses == null) return addresses;
        String[] parts = rawAddresses.split("[\\s,;]+");
        for (String part : parts) {
            String address = part == null ? "" : part.trim();
            if (isValidContractAddress(address) && !addresses.contains(address)) {
                addresses.add(address);
            }
        }
        return addresses;
    }

    public static boolean isValidContractAddress(String address) {
        return address != null && address.matches("(?i)^0x[0-9a-f]{40}$");
    }

    public static String resolveRpcUrl(boolean developerToolsEnabled, String savedRpcUrl) {
        if (!developerToolsEnabled || savedRpcUrl == null) {
            return "";
        }
        return savedRpcUrl.trim();
    }
}
