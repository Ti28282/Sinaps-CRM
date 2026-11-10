package app.sinaps.crm.notify;

import android.app.Activity;
import android.content.ActivityNotFoundException;
import android.content.Intent;
import android.net.Uri;
import android.os.Bundle;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;
import android.widget.Toast;

import java.util.List;

/** Последние сообщения клиента, которые CRM прислала на телефон. Только чтение. */
public class ThreadActivity extends Activity {
    private long chatId;

    @Override protected void onCreate(Bundle b) {
        super.onCreate(b);
        chatId = getIntent().getLongExtra("chatId", 0);
        build();
    }

    @Override protected void onNewIntent(Intent i) {
        super.onNewIntent(i);
        setIntent(i);
        chatId = i.getLongExtra("chatId", 0);
        build();
    }

    private void build() {
        Feed feed = Feed.get(this);
        List<Ev> msgs = feed.thread(chatId);
        Ev last = feed.lastOf(chatId);
        if (last == null) { finish(); return; }

        LinearLayout root = Ui.col(this);
        root.setBackgroundColor(Ui.BG);
        root.setPadding(Ui.dp(this, 16), Ui.dp(this, 14), Ui.dp(this, 16), Ui.dp(this, 16));

        LinearLayout head = Ui.row(this);
        TextView back = Ui.text(this, "‹", 26, Ui.TEXT, false);
        back.setGravity(Gravity.CENTER);
        back.setContentDescription("Назад к уведомлениям");
        back.setBackground(Ui.box(this, Ui.CARD, Ui.LINE, 12));
        back.setOnClickListener(v -> finish());
        head.addView(back, new LinearLayout.LayoutParams(Ui.dp(this, 48), Ui.dp(this, 48)));
        head.addView(Ui.space(this, 12));
        head.addView(Ui.avatar(this, last.client, chatId, 42));
        LinearLayout who = Ui.col(this);
        who.setPadding(Ui.dp(this, 12), 0, 0, 0);
        TextView name = Ui.text(this, last.client, 17, Ui.TEXT, true);
        Ui.ellipsize(name, 1);
        who.addView(name);
        String sub = Ui.channel(last.channel) + (last.stage.isEmpty() ? "" : " · этап «" + last.stage + "»");
        who.addView(Ui.text(this, sub, 12, Ui.MUTED, false));
        head.addView(who, Ui.lp(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1));
        root.addView(head);

        ScrollView scroll = new ScrollView(this);
        LinearLayout list = Ui.col(this);
        list.setPadding(0, Ui.dp(this, 16), 0, Ui.dp(this, 8));
        for (Ev e : msgs) {
            TextView bubble = Ui.text(this, e.msg, 15, Ui.TEXT, false);
            bubble.setPadding(Ui.dp(this, 14), Ui.dp(this, 10), Ui.dp(this, 14), Ui.dp(this, 10));
            bubble.setBackground(Ui.box(this, Ui.CARD, Ui.LINE, 16));
            bubble.setTextIsSelectable(true);
            LinearLayout.LayoutParams p = Ui.lp(ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT, 0);
            p.rightMargin = Ui.dp(this, 48);
            list.addView(bubble, p);
            TextView t = Ui.text(this, Ui.when(e.at).equals(Ui.clock(e.at)) ? Ui.clock(e.at) : Ui.when(e.at) + " " + Ui.clock(e.at), 11, Ui.MUTED, false);
            t.setPadding(Ui.dp(this, 4), Ui.dp(this, 4), 0, Ui.dp(this, 10));
            list.addView(t);
        }
        TextView note = Ui.text(this, "Здесь последние сообщения, которые CRM прислала на телефон. Вся переписка — в CRM и в мессенджере.", 12, Ui.MUTED, false);
        note.setGravity(Gravity.CENTER);
        note.setPadding(Ui.dp(this, 12), Ui.dp(this, 8), Ui.dp(this, 12), 0);
        list.addView(note);
        scroll.addView(list);
        root.addView(scroll, Ui.lp(ViewGroup.LayoutParams.MATCH_PARENT, 0, 1));
        scroll.post(() -> scroll.fullScroll(View.FOCUS_DOWN));

        final String link = replyLink(last);
        if (link != null) {
            TextView reply = Ui.button(this, "Ответить в " + Ui.channel(last.channel), 0);
            reply.setOnClickListener(v -> {
                try {
                    startActivity(new Intent(Intent.ACTION_VIEW, Uri.parse(link)));
                } catch (ActivityNotFoundException ex) {
                    Toast.makeText(this, "Не нашлось приложения, чтобы открыть чат", Toast.LENGTH_LONG).show();
                }
            });
            root.addView(reply, Ui.top(this, Ui.lp(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT, 0), 10));
        }
        TextView read = Ui.button(this, "Отметить прочитанным", 1);
        read.setOnClickListener(v -> { markRead(); finish(); });
        root.addView(read, Ui.top(this, Ui.lp(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT, 0), 8));

        setContentView(root);
    }

    private void markRead() {
        Feed.get(this).markChatRead(chatId);
        LinkService.cancelChat(this, chatId);
    }

    /** Ссылка, которая открывает чат с клиентом в мессенджере; null — открыть нечем. */
    private static String replyLink(Ev e) {
        String phone = e.phone == null ? "" : e.phone.replaceAll("\\D", "");
        if ("wa".equals(e.channel)) return phone.isEmpty() ? null : "https://wa.me/" + phone;
        if ("tg".equals(e.channel)) {
            if (e.username != null && !e.username.isEmpty()) return "https://t.me/" + e.username.replace("@", "");
            return phone.isEmpty() ? null : "https://t.me/+" + phone;
        }
        return null;
    }
}
