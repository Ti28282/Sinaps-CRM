package app.sinaps.crm.notify;

import org.json.JSONObject;

import java.io.Closeable;
import java.io.DataInputStream;
import java.io.IOException;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
import java.security.SecureRandom;

import javax.crypto.Cipher;
import javax.crypto.SecretKeyFactory;
import javax.crypto.spec.GCMParameterSpec;
import javax.crypto.spec.PBEKeySpec;
import javax.crypto.spec.SecretKeySpec;

/**
 * Протокол связи с CRM (версия 1), см. internal/notify/notify.go.
 * Первая строка: "SINAPS1 PAIR" или "SINAPS1 HELLO id". Дальше кадры:
 * длина (4 байта, big-endian) + nonce (12) + AES-256-GCM(JSON).
 */
final class Proto {
    static final int MAX_FRAME = 64 << 10;
    private static final SecureRandom RND = new SecureRandom();

    private Proto() {}

    /** Ключ кадров привязки из одноразового кода с экрана CRM. */
    static byte[] pairKey(String code) throws Exception {
        PBEKeySpec spec = new PBEKeySpec(code.toCharArray(),
                "sinaps-crm-pair-v1".getBytes(StandardCharsets.UTF_8), 120_000, 256);
        return SecretKeyFactory.getInstance("PBKDF2WithHmacSHA256").generateSecret(spec).getEncoded();
    }

    static final class Link implements Closeable {
        final Socket socket;
        private final DataInputStream in;
        private final OutputStream out;
        private final SecretKeySpec key;

        private Link(Socket s, byte[] key) throws IOException {
            this.socket = s;
            this.in = new DataInputStream(s.getInputStream());
            this.out = s.getOutputStream();
            this.key = new SecretKeySpec(key, "AES");
        }

        static Link open(String host, int port, String firstLine, byte[] key, int timeoutMs) throws IOException {
            Socket s = new Socket();
            try {
                s.connect(new InetSocketAddress(host, port), timeoutMs);
                s.setSoTimeout(timeoutMs);
                s.setTcpNoDelay(true);
                s.setKeepAlive(true);
                Link l = new Link(s, key);
                l.out.write((firstLine + "\n").getBytes(StandardCharsets.UTF_8));
                l.out.flush();
                return l;
            } catch (IOException e) {
                try { s.close(); } catch (IOException ignored) {}
                throw e;
            }
        }

        synchronized void send(JSONObject o) throws Exception {
            byte[] nonce = new byte[12];
            RND.nextBytes(nonce);
            Cipher c = Cipher.getInstance("AES/GCM/NoPadding");
            c.init(Cipher.ENCRYPT_MODE, key, new GCMParameterSpec(128, nonce));
            byte[] sealed = c.doFinal(o.toString().getBytes(StandardCharsets.UTF_8));
            ByteBuffer b = ByteBuffer.allocate(4 + nonce.length + sealed.length);
            b.putInt(nonce.length + sealed.length).put(nonce).put(sealed);
            out.write(b.array());
            out.flush();
        }

        JSONObject recv() throws Exception {
            int n = in.readInt();
            if (n < 12 + 16 || n > MAX_FRAME) throw new IOException("неверная длина кадра");
            byte[] body = new byte[n];
            in.readFully(body);
            Cipher c = Cipher.getInstance("AES/GCM/NoPadding");
            c.init(Cipher.DECRYPT_MODE, key, new GCMParameterSpec(128, body, 0, 12));
            byte[] plain = c.doFinal(body, 12, n - 12);
            return new JSONObject(new String(plain, StandardCharsets.UTF_8));
        }

        @Override public void close() {
            try { socket.close(); } catch (IOException ignored) {}
        }
    }
}
