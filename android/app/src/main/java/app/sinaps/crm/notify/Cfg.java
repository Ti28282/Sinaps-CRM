package app.sinaps.crm.notify;

import android.content.Context;
import android.content.SharedPreferences;
import android.util.Base64;

/** Привязка к CRM и настройки показа, которые присылает CRM. */
final class Cfg {
    static final int DEFAULT_PORT = 47815;

    private Cfg() {}

    private static SharedPreferences p(Context c) {
        return c.getApplicationContext().getSharedPreferences("link", Context.MODE_PRIVATE);
    }

    static boolean paired(Context c) {
        return !p(c).getString("id", "").isEmpty() && !p(c).getString("key", "").isEmpty();
    }

    static void savePairing(Context c, String hosts, int port, String id, String keyB64, String crm) {
        p(c).edit().putString("hosts", hosts).putInt("port", port).putString("id", id)
                .putString("key", keyB64).putString("crm", crm).putLong("last", 0).apply();
    }

    static void clear(Context c) { p(c).edit().clear().apply(); }

    static String[] hosts(Context c) {
        String h = p(c).getString("hosts", "");
        return h.isEmpty() ? new String[0] : h.split(",");
    }

    static int port(Context c) { return p(c).getInt("port", DEFAULT_PORT); }
    static String id(Context c) { return p(c).getString("id", ""); }
    static byte[] key(Context c) { return Base64.decode(p(c).getString("key", ""), Base64.DEFAULT); }
    static String crm(Context c) { return p(c).getString("crm", "SINAPS CRM"); }
    static long last(Context c) { return p(c).getLong("last", 0); }
    static void setLast(Context c, long seq) { if (seq > last(c)) p(c).edit().putLong("last", seq).apply(); }

    /** full | name | none — что видно в уведомлении. */
    static String show(Context c) { return p(c).getString("show", "full"); }
    static boolean group(Context c) { return p(c).getBoolean("group", true); }

    static void setConfig(Context c, String crm, String show, Boolean group) {
        SharedPreferences.Editor e = p(c).edit();
        if (crm != null && !crm.isEmpty()) e.putString("crm", crm);
        if (show != null && !show.isEmpty()) e.putString("show", show);
        if (group != null) e.putBoolean("group", group);
        e.apply();
    }
}
