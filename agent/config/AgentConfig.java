package com.example.brokerfi.xc.agent.config;

public final class AgentConfig {
    public static final String LOCAL_HOST = "10.0.2.2";

    public static final String BACKEND_BASE_URL = "http://" + LOCAL_HOST + ":8081";
    public static final String BACKEND_GOLD_API_PREFIX = "/api/v1/gold";

    public static final String BROKER_CHAIN_BASE_URL = "http://" + LOCAL_HOST + ":56741/";

    public static final String IPFS_GATEWAY_URL = "http://" + LOCAL_HOST + ":8083/ipfs/";
    public static final String IPFS_API_ADD_URL = "http://" + LOCAL_HOST + ":5001/api/v0/add";

    public static final String DEEPSEEK_API_URL = "https://api.deepseek.com/chat/completions";
    public static final String DEEPSEEK_MODEL = "deepseek-chat";

    public static final String GOLD_API_URL = "https://api.gold-api.com/price/XAU";
    public static final String SINA_GOLD_URL = "https://hq.sinajs.cn/list=hf_XAU";
    public static final String SINA_REFERER = "https://finance.sina.com.cn";
    public static final String MARKET_USER_AGENT = "Mozilla/5.0 (Linux; Android 12)";
    public static final String FX_USD_CNY_URL = "https://open.er-api.com/v6/latest/USD";

    public static final int HTTP_CONNECT_TIMEOUT_MS = 8000;
    public static final int HTTP_READ_TIMEOUT_MS = 10000;
    public static final int FAST_WRITE_CONNECT_TIMEOUT_MS = 3000;
    public static final int FAST_WRITE_READ_TIMEOUT_MS = 5000;
    public static final int IPFS_CONNECT_TIMEOUT_MS = 5000;
    public static final int IPFS_READ_TIMEOUT_MS = 5000;
    public static final int AI_CONNECT_TIMEOUT_MS = 15000;
    public static final int AI_READ_TIMEOUT_MS = 45000;
    public static final int MARKET_DATA_TIMEOUT_MS = 5000;

    private AgentConfig() {}
}
