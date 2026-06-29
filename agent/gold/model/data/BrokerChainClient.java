package com.example.brokerfi.xc.agent.gold.model.data;

import android.util.Log;

import com.google.gson.Gson;
import com.google.gson.annotations.SerializedName;

import org.bouncycastle.crypto.digests.KeccakDigest;
import org.bouncycastle.crypto.digests.SHA256Digest;
import org.bouncycastle.crypto.params.ECDomainParameters;
import org.bouncycastle.crypto.params.ECPrivateKeyParameters;
import org.bouncycastle.crypto.signers.ECDSASigner;
import org.bouncycastle.jce.ECNamedCurveTable;
import org.bouncycastle.jce.spec.ECNamedCurveParameterSpec;
import org.bouncycastle.math.ec.ECPoint;
import org.bouncycastle.util.encoders.Hex;

import java.io.IOException;
import java.math.BigDecimal;
import java.math.BigInteger;
import java.net.InetAddress;
import java.net.UnknownHostException;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.TimeUnit;
import java.util.UUID;

import okhttp3.Dns;
import okhttp3.MediaType;
import okhttp3.OkHttpClient;
import okhttp3.Request;
import okhttp3.RequestBody;
import okhttp3.Response;

/**
 * BrokerChainClient 客户端核心类
 * 作用：作为 DApp 与底层区块链服务端交互的桥梁。
 */
public class BrokerChainClient {
    private static final String TAG = "BrokerChainClient";
    public static final String SERVICE_HOST = "dash.broker-chain.com";
    private static final String BASE_URL = "https://" + SERVICE_HOST + "/";
    // Android 模拟器的系统 DNS（通常为 10.0.2.3）失效时使用。
    // URL 仍保留域名，因此 HTTPS SNI 和证书主机名校验不会被绕过。
    private static final byte[] SERVICE_FALLBACK_IPV4 =
            new byte[] {43, (byte) 162, 111, (byte) 181};
    private static final MediaType JSON = MediaType.parse("application/json; charset=utf-8");
    private static final Gson gson = new Gson();
    private static final OkHttpClient httpClient = new OkHttpClient.Builder()
            .dns(new BrokerChainDns())
            .connectTimeout(15, TimeUnit.SECONDS)
            .readTimeout(120, TimeUnit.SECONDS)
            .writeTimeout(30, TimeUnit.SECONDS)
            // 请求带一次性 UUID 和签名，原样重试会触发 replay attack。
            .retryOnConnectionFailure(false)
            .build();

    private static final class BrokerChainDns implements Dns {
        @Override
        public List<InetAddress> lookup(String hostname) throws UnknownHostException {
            try {
                return Dns.SYSTEM.lookup(hostname);
            } catch (UnknownHostException systemDnsError) {
                if (!SERVICE_HOST.equalsIgnoreCase(hostname)) throw systemDnsError;
                InetAddress fallback = InetAddress.getByAddress(hostname, SERVICE_FALLBACK_IPV4);
                Log.w(TAG, "系统 DNS 解析失败，BrokerChain 使用备用地址 "
                        + fallback.getHostAddress());
                return Collections.singletonList(fallback);
            }
        }
    }

    private static String doPost(String endpoint, Object requestBody) throws Exception {
        String jsonInputString = gson.toJson(requestBody);
        Request request = new Request.Builder()
                .url(BASE_URL + endpoint)
                .header("Accept", "application/json")
                .post(RequestBody.create(jsonInputString, JSON))
                .build();

        try (Response response = httpClient.newCall(request).execute()) {
            String text = response.body() != null ? response.body().string() : "";
            if (!response.isSuccessful()) {
                throw new IOException("BrokerChain HTTP " + response.code() + ": " + text);
            }
            Log.d(TAG, "Request to " + endpoint + " success: " + text);
            return text;
        }
    }

    private static final BigInteger SECP256K1_N = new BigInteger(
        "FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141", 16);

    public static String getPublicKeyFromPrivateKey(String privateKeyHex) {
        if (privateKeyHex.startsWith("0x")) privateKeyHex = privateKeyHex.substring(2);
        BigInteger privateKey = new BigInteger(privateKeyHex, 16);

        if (privateKey.compareTo(BigInteger.ZERO) <= 0 || privateKey.compareTo(SECP256K1_N) >= 0) {
            Log.e(TAG, "getPublicKeyFromPrivateKey: private key out of valid range (0 < k < n)");
            return null;
        }

        ECNamedCurveParameterSpec spec = ECNamedCurveTable.getParameterSpec("secp256k1");
        ECPoint publicPoint = spec.getG().multiply(privateKey);
        byte[] encoded = publicPoint.getEncoded(false);
        return Hex.toHexString(encoded);
    }

    public static String[] signECDSA(String privateKeyHex, String data) {
        if (privateKeyHex.startsWith("0x")) privateKeyHex = privateKeyHex.substring(2);
        BigInteger privateKey = new BigInteger(privateKeyHex, 16);

        ECNamedCurveParameterSpec spec = ECNamedCurveTable.getParameterSpec("secp256k1");
        ECDomainParameters domainParameters = new ECDomainParameters(spec.getCurve(), spec.getG(), spec.getN());
        ECDSASigner signer = new ECDSASigner();
        signer.init(true, new ECPrivateKeyParameters(privateKey, domainParameters));

        SHA256Digest digest = new SHA256Digest();
        byte[] dataBytes = data.getBytes();
        digest.update(dataBytes, 0, dataBytes.length);
        byte[] hash = new byte[digest.getDigestSize()];
        digest.doFinal(hash, 0);

        BigInteger[] rs = signer.generateSignature(hash);
        return new String[]{
                Hex.toHexString(rs[0].toByteArray()),
                Hex.toHexString(rs[1].toByteArray())
        };
    }

    public static String getAddress(String privateKey) {
        try {
            if (privateKey == null || privateKey.isEmpty()) {
                Log.e(TAG, "getAddress: privateKey is null or empty");
                return "";
            }
            String cleanKey = privateKey.startsWith("0x") ? privateKey.substring(2) : privateKey;
            if (cleanKey.length() != 64) {
                Log.e(TAG, "getAddress: invalid private key length: " + cleanKey.length());
                return "";
            }
            String publicKey = getPublicKeyFromPrivateKey(cleanKey);
            if (publicKey == null || publicKey.isEmpty()) {
                Log.e(TAG, "getAddress: failed to derive public key");
                return "";
            }
            byte[] decode = Hex.decode(publicKey);

            KeccakDigest keccakDigest = new KeccakDigest(256);
            keccakDigest.update(decode, 1, decode.length - 1);
            byte[] keccakHash = new byte[keccakDigest.getDigestSize()];
            keccakDigest.doFinal(keccakHash, 0);

            byte[] addressBytes = new byte[20];
            System.arraycopy(keccakHash, keccakHash.length - 20, addressBytes, 0, 20);
            return "0x" + Hex.toHexString(addressBytes);
        } catch (Exception e) {
            Log.e(TAG, "getAddress: exception - " + e.getMessage());
            e.printStackTrace();
            return "";
        }
    }

    public static String sendEthCall(String privateKey, String to, String data) throws Exception {
        String uuid = UUID.randomUUID().toString();
        String value = "0x0";
        String thedata = to + data + value + uuid;
        String[] sign = signECDSA(privateKey, thedata);

        CallReq req = new CallReq();
        req.setPublicKey(getPublicKeyFromPrivateKey(privateKey));
        req.setRandomStr(uuid);
        req.setTo(to);
        req.setData(data);
        req.setValue(value);
        req.setSign1(sign[0]);
        req.setSign2(sign[1]);

        return doPost("eth_call", req);
    }

    public static String sendEthTx(String privateKey, String to, String data, String value) throws Exception {
        // 将 Gas Limit 调高至 800万 (0x7a1200)，防止因字符串过长导致部署失败
        String gas = "0x7a1200";
        String finalValue = (value == null || value.isEmpty() || value.equals("0")) ? "0x0" : value;
        if (!finalValue.startsWith("0x")) finalValue = "0x" + finalValue;

        String uuid = UUID.randomUUID().toString();
        String thedata = to + data + finalValue + gas + uuid;
        String[] sign = signECDSA(privateKey, thedata);

        SendETHTXReq req = new SendETHTXReq();
        req.setPublicKey(getPublicKeyFromPrivateKey(privateKey));
        req.setRandomStr(uuid);
        req.setTo(to);
        req.setData(data);
        req.setValue(finalValue);
        req.setGas(gas);
        req.setSign1(sign[0]);
        req.setSign2(sign[1]);

        return doPost("eth_sendTransaction", req);
    }

    public static ReturnAccountState getAddrAndBalance(String privateKey) throws Exception {
        String uuid = UUID.randomUUID().toString();
        String rawAddress = getAddress(privateKey);
        String address = rawAddress.startsWith("0x") ? rawAddress.substring(2) : rawAddress;

        String data = uuid + address;
        String[] sign = signECDSA(privateKey, data);

        QueryReq queryReq = new QueryReq();
        queryReq.setPublicKey(getPublicKeyFromPrivateKey(privateKey));
        queryReq.setRandomStr(uuid);
        queryReq.setSign1(sign[0]);
        queryReq.setSign2(sign[1]);
        queryReq.setUUID(address);

        String result = doPost("query-g", queryReq);
        ReturnAccountState state = gson.fromJson(result, ReturnAccountState.class);

        if (state != null && state.getBalance() != null) {
            BigDecimal a = new BigDecimal(state.getBalance());
            BigDecimal b = new BigDecimal("1000000000000000000");
            state.setBalance(a.divide(b).toString());
        }
        return state;
    }

    public static class CallReq {
        @SerializedName("PublicKey")
        private String PublicKey;
        @SerializedName("RandomStr")
        private String RandomStr;
        @SerializedName("To")
        private String To;
        @SerializedName("data")
        private String data;
        @SerializedName("value")
        private String value;
        @SerializedName("Sign1")
        private String Sign1;
        @SerializedName("Sign2")
        private String Sign2;

        public void setPublicKey(String p) { PublicKey = p; }
        public void setRandomStr(String r) { RandomStr = r; }
        public void setTo(String t) { To = t; }
        public void setData(String d) { data = d; }
        public void setValue(String v) { value = v; }
        public void setSign1(String s) { Sign1 = s; }
        public void setSign2(String s) { Sign2 = s; }
    }

    public static class SendETHTXReq {
        private String PublicKey;
        private String RandomStr;
        private String To;
        @SerializedName("data")
        private String data;
        @SerializedName("value")
        private String value;
        private String Gas;
        private String Sign1;
        private String Sign2;

        public void setPublicKey(String p) { PublicKey = p; }
        public void setRandomStr(String r) { RandomStr = r; }
        public void setTo(String t) { To = t; }
        public void setData(String d) { data = d; }
        public void setValue(String v) { value = v; }
        public void setGas(String g) { Gas = g; }
        public void setSign1(String s) { Sign1 = s; }
        public void setSign2(String s) { Sign2 = s; }
    }

    public static class QueryReq {
        private String PublicKey, RandomStr, Sign1, Sign2, UUID;
        public void setPublicKey(String p) { PublicKey = p; }
        public void setRandomStr(String r) { RandomStr = r; }
        public void setSign1(String s) { Sign1 = s; }
        public void setSign2(String s) { Sign2 = s; }
        public void setUUID(String u) { UUID = u; }
    }

    public static class ReturnAccountState {
        @SerializedName("account")
        private String AccountAddr;
        @SerializedName("balance")
        private String Balance;
        public String getAccountAddr() { return AccountAddr; }
        public void setAccountAddr(String a) { AccountAddr = a; }
        public String getBalance() { return Balance; }
        public void setBalance(String b) { Balance = b; }
    }
}
