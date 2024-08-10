import altair as alt
import pandas as pd
import streamlit as st

st.title('Links Stats')

conn = st.connection('sql', type='sql', url="sqlite:///pb_data/data.db")


@st.cache_resource
def load_items():
    return pd.DataFrame(conn.query('select * from items'))

@st.cache_resource
def load_logs():
    return pd.DataFrame(conn.query('select * from logs'))


items = load_items()
logs = load_logs()

# logs data sanitization
logs['created'] = pd.to_datetime(logs['created'], format="mixed", utc=True)
items['created'] = pd.to_datetime(items['created'], format="mixed", utc=True)
# logs['created'].dt.tz_localize(None)

col1, col2, col3, col4 = st.columns(4)
col1.metric(
    label="Average Usage per alias",
    value=logs["alias"].value_counts().mean().round(decimals=1),
)

items_creation_span = items['created'].max() - items['created'].min()
total_items = items['alias'].nunique()
items_per_day = round(total_items / items_creation_span.days, 2)

col2.metric(
    label="New alias every",
    value=f'{items_per_day} days',
)

grouped_by_alias = logs['alias'].value_counts()

st.header('Top N Links')
n_largest = st.slider('Top N', 5, 20, 15)
max_rounded = round(grouped_by_alias.max(), -3)
upper_count_limit = st.slider(
    label='Upper Limit for Count',
    min_value=0,
    max_value=max_rounded,
    value=max_rounded,
    step=500,
)
col1, col2 = st.columns(2)

col1.altair_chart(
    alt.Chart(
        grouped_by_alias[logs['alias'].value_counts() <= upper_count_limit]
        .nlargest(n_largest)
        .reset_index(name='count')
    )
    .mark_arc()
    .encode(
        theta='count',
        color=alt.Color('alias', legend=None),
    ),
    use_container_width=True,
)

col2.altair_chart(
    alt.Chart(
        grouped_by_alias[logs['alias'].value_counts() <= upper_count_limit]
        .nlargest(n_largest)
        .reset_index(name='count')
        .sort_values('count', ascending=False)
    )
    .mark_bar()
    .encode(
        x='count:Q',
        y=alt.Y('alias:N').sort('-x'),
        color=alt.Color('alias', legend=None),
    ),
    use_container_width=True,
)

st.header('Usage over time')
earliest_log_created = logs['created'].min()
latest_log_created = logs['created'].max()
log_created_dates = (earliest_log_created.date(), latest_log_created.date())
earliest_date, latest_date = st.slider('Date range', *log_created_dates, log_created_dates)
date_mask = (logs['created'].dt.date >= earliest_date) & (logs['created'].dt.date <= latest_date)

logs_grouped_by_created = (
    logs
        .loc[date_mask]
        .groupby(logs['created'].dt.date).size().reset_index(name='count')
)

st.altair_chart(
    alt.Chart(logs_grouped_by_created)
    .mark_bar()
    .encode(
        y='count:Q',
        x='created:T'
    ),
    use_container_width=True,
)

selected_aliases = st.multiselect('Aliases', logs['alias'].unique(), default='g')

st.altair_chart(
    alt.Chart(
        logs[logs['alias'].isin(selected_aliases)]
            .loc[date_mask]
            .groupby([logs['alias'], logs['created'].dt.date]).size().reset_index(name='count')
    )
    .mark_bar()
    .encode(
        y='count:Q',
        x='created:T',
        color='alias',
    ),
    use_container_width=True,
)

st.header('Creation of new aliases over time')

earliest_item_created = items['created'].min()
latest_item_created = items['created'].max()
item_created_dates = (earliest_item_created.date(), latest_item_created.date())
earliest_item_date, latest_item_date = st.slider('Date range', *item_created_dates, item_created_dates)
date_mask = (items['created'].dt.date >= earliest_item_date) & (items['created'].dt.date <= latest_item_date)

items_grouped_by_created = (
    items
        .loc[date_mask]
        .groupby(items['created'].dt.date).size().reset_index(name='count')
)

st.altair_chart(
    alt.Chart(items_grouped_by_created)
    .mark_bar()
    .encode(
        y='count:Q',
        x='created:T'
    ),
    use_container_width=True,
)


st.header('Usage by days of the week')

st.text('todo')

st.header('Other')

st.markdown(
"""
- What is the average number of arguments used per alias?
- What percentage of aliases have never been used?
- Which alias has the most diverse set of arguments?
- What is the distribution of alias lengths (number of characters)?
- Are there any correlations between the time of day and specific alias usage?
- What si the average lifespan of an alias (time between creation and last use)?
"""
)
