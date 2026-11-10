package app.sinaps.crm.notify;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Context;
import android.content.Intent;
import android.content.pm.ServiceInfo;
import android.net.ConnectivityManager;
import android.net.Network;
import android.os.Build;
import android.os.IBinder;

import org.json.JSONObject;

/**
 * Держит соединение с CRM в фоне: подключается к компьютеру, принимает события,
 * кладёт их в ленту и показывает уведомления. Клиентам ничего не отправляет.
 */
public class LinkService extends Service {
    static final String CHANGED = "app.sinaps.crm.notify.CHANGED";
    static final int OFF = 0, WAITING = 1, ONLINE = 2;
    /** Состояние связи для экранов приложения. */
    static volatile int state = OFF;

    private static final String CH_MSG = "msgs", CH_EVENT = "events", CH_LINK = "link";
    private static final int LINK_NOTE = 1;
    private static final long FRESH_SEC = 600; // старее 10 минут — только в ленту, без звука

    private final Object lock = new Object();
    private volatile boolean running;
    private volatile Proto.Link current;
    private Thread worker;
    private ConnectivityManager.NetworkCallback netCb;

    static void start(Context c) {
        c.startForegroundService(new Intent(c, LinkService.class));
    }

    @Override public IBinder onBind(Intent i) { return null; }

    @Override public void onCreate() {
        super.onCreate();
        channels();
        Notification n = linkNote();
        if (Build.VERSION.SDK_INT >= 34) {
            startForeground(LINK_NOTE, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_REMOTE_MESSAGING);
        } else {
            startForeground(LINK_NOTE, n);
        }
        running = true;
        worker = new Thread(this::loop, "sinaps-link");
        worker.start();
        // появилась сеть (вернулся домой, включил Wi-Fi) — подключаемся сразу, не дожидаясь паузы
        ConnectivityManager cm = getSystemService(ConnectivityManager.class);
        if (cm != null) {
            netCb = new ConnectivityManager.NetworkCallback() {
                @Override public void onAvailable(Network network) { wake(); }
            };
            try { cm.registerDefaultNetworkCallback(netCb); } catch (RuntimeException ignored) { netCb = null; }
        }
    }

    @Override public int onStartCommand(Intent i, int flags, int startId) {
        wake();
        return START_STICKY;
    }

    @Override public void onDestroy() {
        running = false;
        Proto.Link l = current;
        if (l != null) l.close();
        wake();
        ConnectivityManager cm = getSystemService(ConnectivityManager.class);
        if (cm != null && netCb != null) {
            try { cm.unregisterNetworkCallback(netCb); } catch (RuntimeException ignored) {}
        }
        setState(OFF);
        super.onDestroy();
    }

    private void wake() { synchronized (lock) { lock.notifyAll(); } }

    // ---------- соединение ----------

    private void loop() {
        long pause = 3000;
        while (running) {
            if (!Cfg.paired(this)) { stopSelf(); return; }
            boolean wasOnline = false;
            for (String host : Cfg.hosts(this)) {
                if (!running) return;
                try (Proto.Link l = Proto.Link.open(host, Cfg.port(this), "SINAPS1 HELLO " + Cfg.id(this), Cfg.key(this), 6000)) {
                    current = l;
                    l.send(new JSONObject().put("t", "hello").put("last", Cfg.last(this)).put("name", deviceName()));
                    l.socket.setSoTimeout(75_000); // CRM шлёт ping каждые 25 секунд
                    while (running) {
                        JSONObject f = l.recv();
                        wasOnline = true;
                        handle(l, f);
                    }
                } catch (Exception ignored) {
                    // компьютер выключен, другая сеть или телефон отвязали — пробуем следующий адрес и ждём
                } finally {
                    current = null;
                }
                if (wasOnline) break;
            }
            if (!running) return;
            setState(WAITING);
            pause = wasOnline ? 3000 : Math.min(pause * 2, 60_000);
            synchronized (lock) {
                try { lock.wait(pause); } catch (InterruptedException e) { return; }
            }
        }
    }

    private void handle(Proto.Link l, JSONObject f) throws Exception {
        switch (f.optString("t")) {
            case "welcome":
            case "config":
                Cfg.setConfig(this, f.optString("crm"), f.optString("show"), f.has("group") ? f.optBoolean("group") : null);
                setState(ONLINE);
                break;
            case "ping":
                l.send(new JSONObject().put("t", "pong"));
                break;
            case "event":
                JSONObject o = f.optJSONObject("event");
                if (o == null) break;
                Ev e = Feed.get(this).add(o);
                Cfg.setLast(this, o.optLong("seq"));
                if (e == null) break;
                boolean fresh = e.at == 0 || e.at >= System.currentTimeMillis() / 1000 - FRESH_SEC;
                if (!o.optBoolean("quiet") && fresh) show(e);
                sendBroadcast(new Intent(CHANGED).setPackage(getPackageName()));
                break;
            default:
                break;
        }
    }

    private void setState(int s) {
        if (state == s) return;
        state = s;
        if (s != OFF) {
            NotificationManager nm = getSystemService(NotificationManager.class);
            if (nm != null) nm.notify(LINK_NOTE, linkNote());
        }
        sendBroadcast(new Intent(CHANGED).setPackage(getPackageName()));
    }

    static String deviceName() {
        String m = Build.MANUFACTURER == null ? "" : Build.MANUFACTURER.trim();
        String d = Build.MODEL == null ? "" : Build.MODEL.trim();
        if (!m.isEmpty()) m = m.substring(0, 1).toUpperCase() + m.substring(1);
        String n = d.toLowerCase().startsWith(m.toLowerCase()) ? d : (m + " " + d).trim();
        return n.isEmpty() ? "Android" : n;
    }

    // ---------- уведомления ----------

    private void channels() {
        NotificationManager nm = getSystemService(NotificationManager.class);
        if (nm == null) return;
        NotificationChannel msg = new NotificationChannel(CH_MSG, "Сообщения клиентов", NotificationManager.IMPORTANCE_HIGH);
        msg.setDescription("Клиент написал, новый клиент, ответ на рассылку");
        NotificationChannel ev = new NotificationChannel(CH_EVENT, "События CRM", NotificationManager.IMPORTANCE_DEFAULT);
        ev.setDescription("Напоминания и состояние CRM");
        NotificationChannel link = new NotificationChannel(CH_LINK, "Связь с CRM", NotificationManager.IMPORTANCE_MIN);
        link.setDescription("Постоянная строка: приложение работает в фоне");
        link.setShowBadge(false);
        nm.createNotificationChannel(msg);
        nm.createNotificationChannel(ev);
        nm.createNotificationChannel(link);
    }

    private Notification linkNote() {
        PendingIntent open = PendingIntent.getActivity(this, 0, new Intent(this, MainActivity.class),
                PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
        return new Notification.Builder(this, CH_LINK)
                .setSmallIcon(R.drawable.ic_stat)
                .setContentTitle(state == ONLINE ? "На связи с " + Cfg.crm(this) : "Нет связи с компьютером")
                .setContentText(state == ONLINE ? "Сообщения клиентов придут сюда" : "Подключимся, когда CRM будет в сети")
                .setOngoing(true)
                .setContentIntent(open)
                .build();
    }

    private void show(Ev e) {
        NotificationManager nm = getSystemService(NotificationManager.class);
        if (nm == null) return;
        String mode = Cfg.show(this);
        String title = e.title, text = e.text;
        int id = (int) (1000 + e.seq % 1_000_000);
        boolean chat = e.isChat() && e.chatId != 0;
        String sub = null;
        if (chat && Cfg.group(this)) {
            // несколько сообщений подряд от одного клиента — одно уведомление, оно обновляется
            id = (int) (2_000_000 + e.chatId % 1_000_000);
            int n = Feed.get(this).unread(e.chatId);
            if (n > 1) sub = n + " " + plural(n, "сообщение", "сообщения", "сообщений");
        }
        if (e.isChat() || "silent".equals(e.kind) || "wait".equals(e.kind)) {
            if ("name".equals(mode)) {
                text = sub == null ? "" : sub;
            } else if ("none".equals(mode)) {
                title = e.isChat() ? "Новое сообщение" : "Напоминание из CRM";
                text = "Открой приложение, чтобы прочитать";
            }
        }
        Intent open = chat
                ? new Intent(this, ThreadActivity.class).putExtra("chatId", e.chatId)
                : new Intent(this, MainActivity.class);
        open.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TOP);
        PendingIntent pi = PendingIntent.getActivity(this, id, open, PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
        Notification.Builder b = new Notification.Builder(this, e.isChat() || "alarm".equals(e.kind) ? CH_MSG : CH_EVENT)
                .setSmallIcon(R.drawable.ic_stat)
                .setColor(Ui.ACCENT)
                .setContentTitle(title)
                .setContentText(text)
                .setStyle(new Notification.BigTextStyle().bigText(text))
                .setWhen(e.at > 0 ? e.at * 1000 : System.currentTimeMillis())
                .setShowWhen(true)
                .setAutoCancel(true)
                .setCategory(e.isChat() ? Notification.CATEGORY_MESSAGE : Notification.CATEGORY_REMINDER)
                .setContentIntent(pi);
        if (sub != null && !"name".equals(mode)) b.setSubText(sub);
        try {
            nm.notify(id, b.build());
        } catch (SecurityException ignored) {
            // уведомления запрещены в настройках телефона — событие всё равно в ленте
        }
    }

    /** Убирает уведомление клиента из шторки (его прочитали в приложении). */
    static void cancelChat(Context c, long chatId) {
        NotificationManager nm = c.getSystemService(NotificationManager.class);
        if (nm != null) nm.cancel((int) (2_000_000 + chatId % 1_000_000));
    }

    static String plural(int n, String one, String few, String many) {
        int a = n % 10, b = n % 100;
        return a == 1 && b != 11 ? one : a >= 2 && a <= 4 && (b < 10 || b >= 20) ? few : many;
    }
}
