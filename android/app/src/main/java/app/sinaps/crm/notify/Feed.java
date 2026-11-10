package app.sinaps.crm.notify;

import android.content.ContentValues;
import android.content.Context;
import android.database.Cursor;
import android.database.sqlite.SQLiteDatabase;
import android.database.sqlite.SQLiteOpenHelper;

import org.json.JSONObject;

import java.util.ArrayList;
import java.util.List;

/** Лента уведомлений на телефоне: последние события, которые прислала CRM. */
final class Feed extends SQLiteOpenHelper {
    private static final int KEEP = 500;
    private static Feed inst;

    static synchronized Feed get(Context c) {
        if (inst == null) inst = new Feed(c.getApplicationContext());
        return inst;
    }

    private Feed(Context c) { super(c, "feed.db", null, 1); }

    @Override public void onCreate(SQLiteDatabase db) {
        db.execSQL("CREATE TABLE events(seq INTEGER PRIMARY KEY, kind TEXT, title TEXT, text TEXT, chat_id INTEGER,"
                + " client TEXT, channel TEXT, username TEXT, phone TEXT, stage TEXT, msg TEXT, at INTEGER, read INTEGER NOT NULL DEFAULT 0)");
        db.execSQL("CREATE INDEX events_chat ON events(chat_id, seq)");
    }

    @Override public void onUpgrade(SQLiteDatabase db, int a, int b) {}

    /** Сохраняет событие. null — такое уже есть (CRM прислала повторно). */
    synchronized Ev add(JSONObject o) {
        Ev e = new Ev();
        e.seq = o.optLong("seq");
        e.kind = o.optString("kind");
        e.title = o.optString("title");
        e.text = o.optString("text");
        e.chatId = o.optLong("chatId");
        e.client = o.optString("client");
        e.channel = o.optString("channel");
        e.username = o.optString("username");
        e.phone = o.optString("phone");
        e.stage = o.optString("stage");
        e.msg = o.optString("msg");
        e.at = o.optLong("at");
        if (e.seq <= 0) return null;
        ContentValues v = new ContentValues();
        v.put("seq", e.seq); v.put("kind", e.kind); v.put("title", e.title); v.put("text", e.text);
        v.put("chat_id", e.chatId); v.put("client", e.client); v.put("channel", e.channel);
        v.put("username", e.username); v.put("phone", e.phone); v.put("stage", e.stage);
        v.put("msg", e.msg); v.put("at", e.at);
        SQLiteDatabase db = getWritableDatabase();
        if (db.insertWithOnConflict("events", null, v, SQLiteDatabase.CONFLICT_IGNORE) < 0) return null;
        db.execSQL("DELETE FROM events WHERE seq <= (SELECT MAX(seq) FROM events) - " + KEEP);
        return e;
    }

    /** filter: all | chat | event. Новые сверху. */
    synchronized List<Ev> list(String filter) {
        String where = "chat".equals(filter) ? "kind IN ('msg','fresh','reply')"
                : "event".equals(filter) ? "kind NOT IN ('msg','fresh','reply')" : null;
        return query(where, null, "seq DESC", "200");
    }

    /** Сообщения одного клиента, старые сверху. */
    synchronized List<Ev> thread(long chatId) {
        return query("chat_id=? AND msg<>''", new String[]{String.valueOf(chatId)}, "seq ASC", null);
    }

    synchronized Ev lastOf(long chatId) {
        List<Ev> l = query("chat_id=?", new String[]{String.valueOf(chatId)}, "seq DESC", "1");
        return l.isEmpty() ? null : l.get(0);
    }

    synchronized int unread(long chatId) {
        try (Cursor c = getReadableDatabase().rawQuery(
                "SELECT COUNT(*) FROM events WHERE chat_id=? AND read=0 AND kind IN ('msg','fresh','reply')",
                new String[]{String.valueOf(chatId)})) {
            return c.moveToFirst() ? c.getInt(0) : 0;
        }
    }

    synchronized void markChatRead(long chatId) {
        getWritableDatabase().execSQL("UPDATE events SET read=1 WHERE chat_id=?", new Object[]{chatId});
    }

    synchronized void markRead(long seq) {
        getWritableDatabase().execSQL("UPDATE events SET read=1 WHERE seq=?", new Object[]{seq});
    }

    synchronized void markAllRead() { getWritableDatabase().execSQL("UPDATE events SET read=1"); }

    synchronized void clear() { getWritableDatabase().execSQL("DELETE FROM events"); }

    private List<Ev> query(String where, String[] args, String order, String limit) {
        List<Ev> out = new ArrayList<>();
        try (Cursor c = getReadableDatabase().query("events", null, where, args, null, null, order, limit)) {
            while (c.moveToNext()) {
                Ev e = new Ev();
                e.seq = c.getLong(c.getColumnIndexOrThrow("seq"));
                e.kind = c.getString(c.getColumnIndexOrThrow("kind"));
                e.title = c.getString(c.getColumnIndexOrThrow("title"));
                e.text = c.getString(c.getColumnIndexOrThrow("text"));
                e.chatId = c.getLong(c.getColumnIndexOrThrow("chat_id"));
                e.client = c.getString(c.getColumnIndexOrThrow("client"));
                e.channel = c.getString(c.getColumnIndexOrThrow("channel"));
                e.username = c.getString(c.getColumnIndexOrThrow("username"));
                e.phone = c.getString(c.getColumnIndexOrThrow("phone"));
                e.stage = c.getString(c.getColumnIndexOrThrow("stage"));
                e.msg = c.getString(c.getColumnIndexOrThrow("msg"));
                e.at = c.getLong(c.getColumnIndexOrThrow("at"));
                e.read = c.getInt(c.getColumnIndexOrThrow("read")) != 0;
                out.add(e);
            }
        }
        return out;
    }
}
